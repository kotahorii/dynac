package query

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"

	"github.com/kotahorii/dynac/internal/partiql"
)

type Kind int

const (
	KindOne Kind = iota
	KindMany
	KindExec
)

type Annotations struct {
	Limit      *int
	NoLimit    bool
	Index      *IndexDef
	Projection string
	Model      string
}

type IndexDef struct {
	Name string
	PK   []IndexAttr
	SK   []IndexAttr
}

type IndexAttr struct {
	Name     string
	TypeHint string
}

type Query struct {
	Name        string
	Kind        Kind
	SQL         string
	Stmt        partiql.Statement
	Annotations Annotations
	File        string
	Line        int
}

type ValidateOptions struct {
	Table string
	PK    string
	SK    string
}

func LoadFiles(paths []string) ([]Query, error) {
	var all []Query
	seen := map[string]Query{}
	for _, path := range paths {
		qs, err := LoadFile(path)
		if err != nil {
			return nil, err
		}
		for _, q := range qs {
			if prev, ok := seen[q.Name]; ok {
				return nil, fmt.Errorf("%s:%d: duplicate query name %s (also in %s:%d)", q.File, q.Line, q.Name, prev.File, prev.Line)
			}
			seen[q.Name] = q
			all = append(all, q)
		}
	}
	return all, nil
}

func DiscoverQueryFiles(root string) ([]string, error) {
	var files []string
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		if strings.HasSuffix(d.Name(), ".partiql") {
			files = append(files, path)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Strings(files)
	return files, nil
}

func LoadFile(path string) ([]Query, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	lines := strings.Split(string(data), "\n")
	var (
		queries  []Query
		current  *Query
		sqlLines []string
		seen     = map[string]bool{}
	)
	flush := func() error {
		if current == nil {
			return nil
		}
		sql := strings.TrimSpace(strings.Join(sqlLines, "\n"))
		if sql == "" {
			return fmt.Errorf("%s:%d: missing SQL body", current.File, current.Line)
		}
		stmt, err := partiql.Parse(sql)
		if err != nil {
			return fmt.Errorf("%s:%d: %w", current.File, current.Line, err)
		}
		current.SQL = sql
		current.Stmt = stmt
		if seen[current.Name] {
			return fmt.Errorf("%s:%d: duplicate query name %s", current.File, current.Line, current.Name)
		}
		seen[current.Name] = true
		queries = append(queries, *current)
		current = nil
		sqlLines = nil
		return nil
	}
	for i, line := range lines {
		lineNo := i + 1
		trimmed := strings.TrimSpace(line)
		if after, ok := strings.CutPrefix(trimmed, "--"); ok {
			body := strings.TrimSpace(after)
			if strings.HasPrefix(body, "name:") {
				if err := flush(); err != nil {
					return nil, err
				}
				name, kind, err := parseName(body)
				if err != nil {
					return nil, fmt.Errorf("%s:%d: %w", path, lineNo, err)
				}
				current = &Query{Name: name, Kind: kind, File: path, Line: lineNo}
				continue
			}
			if current != nil && strings.HasPrefix(body, "@") {
				if err := applyAnnotation(&current.Annotations, body); err != nil {
					return nil, fmt.Errorf("%s:%d: %w", path, lineNo, err)
				}
				continue
			}
			continue
		}
		if current == nil {
			if trimmed == "" {
				continue
			}
			return nil, fmt.Errorf("%s:%d: SQL found before query name", path, lineNo)
		}
		if trimmed == "" {
			continue
		}
		sqlLines = append(sqlLines, trimmed)
	}
	if err := flush(); err != nil {
		return nil, err
	}
	return queries, nil
}

func parseName(body string) (string, Kind, error) {
	body = strings.TrimSpace(strings.TrimPrefix(body, "name:"))
	parts := strings.Fields(body)
	if len(parts) < 2 {
		return "", KindExec, fmt.Errorf("invalid name annotation")
	}
	name := parts[0]
	kindStr := parts[1]
	var kind Kind
	switch kindStr {
	case ":one":
		kind = KindOne
	case ":many":
		kind = KindMany
	case ":exec":
		kind = KindExec
	default:
		return "", KindExec, fmt.Errorf("unknown query kind %s", kindStr)
	}
	return name, kind, nil
}

func applyAnnotation(a *Annotations, body string) error {
	fields := strings.Fields(body)
	if len(fields) == 0 {
		return nil
	}
	switch fields[0] {
	case "@limit":
		if len(fields) != 2 {
			return fmt.Errorf("@limit requires a value")
		}
		if a.Limit != nil {
			return fmt.Errorf("@limit already set")
		}
		v, err := strconv.Atoi(fields[1])
		if err != nil || v <= 0 {
			return fmt.Errorf("@limit must be positive integer")
		}
		a.Limit = &v
	case "@nolimit":
		if a.NoLimit {
			return fmt.Errorf("@nolimit already set")
		}
		a.NoLimit = true
	case "@index":
		idx, err := parseIndex(body)
		if err != nil {
			return err
		}
		a.Index = idx
	case "@projection":
		if len(fields) != 2 {
			return fmt.Errorf("@projection requires a value")
		}
		a.Projection = fields[1]
	case "@model":
		if len(fields) != 2 {
			return fmt.Errorf("@model requires a type name")
		}
		a.Model = fields[1]
	default:
		return fmt.Errorf("unknown annotation %s", fields[0])
	}
	return nil
}

func parseIndex(body string) (*IndexDef, error) {
	rest := strings.TrimSpace(strings.TrimPrefix(body, "@index"))
	if rest == "" {
		return nil, fmt.Errorf("@index requires name")
	}
	parts := strings.Fields(rest)
	if len(parts) < 2 {
		return nil, fmt.Errorf("@index requires name and pk/sk")
	}
	name := parts[0]
	pkList, err := extractParenList(rest, "pk")
	if err != nil {
		return nil, err
	}
	skList, err := extractParenList(rest, "sk")
	if err != nil {
		return nil, err
	}
	pk, err := parseIndexAttrs(pkList)
	if err != nil {
		return nil, fmt.Errorf("pk: %w", err)
	}
	sk, err := parseIndexAttrs(skList)
	if err != nil {
		return nil, fmt.Errorf("sk: %w", err)
	}
	return &IndexDef{Name: name, PK: pk, SK: sk}, nil
}

func extractParenList(s, key string) (string, error) {
	idx := strings.Index(s, key+"(")
	if idx < 0 {
		return "", fmt.Errorf("missing %s(...)", key)
	}
	start := idx + len(key) + 1
	end := strings.Index(s[start:], ")")
	if end < 0 {
		return "", fmt.Errorf("unterminated %s(...)", key)
	}
	return s[start : start+end], nil
}

func parseIndexAttrs(raw string) ([]IndexAttr, error) {
	items := strings.Split(raw, ",")
	var attrs []IndexAttr
	for _, item := range items {
		item = strings.TrimSpace(item)
		if item == "" {
			return nil, fmt.Errorf("empty attribute")
		}
		name := item
		typeHint := ""
		if strings.Contains(item, ":") {
			parts := strings.SplitN(item, ":", 2)
			name = strings.TrimSpace(parts[0])
			typeHint = strings.TrimSpace(parts[1])
		}
		if name == "" {
			return nil, fmt.Errorf("empty attribute")
		}
		if typeHint != "" {
			switch strings.ToUpper(typeHint) {
			case "S", "N", "B":
				// ok
			default:
				return nil, fmt.Errorf("invalid type hint %s", typeHint)
			}
		}
		attrs = append(attrs, IndexAttr{Name: name, TypeHint: typeHint})
	}
	return attrs, nil
}

func (q Query) Validate(opts ValidateOptions) error {
	switch stmt := q.Stmt.(type) {
	case partiql.SelectStmt:
		return validateSelect(q, stmt, opts)
	case partiql.InsertStmt:
		if q.Kind != KindExec {
			return fmt.Errorf("%s: INSERT must be :exec", q.Name)
		}
		if err := validateWriteBase(q, stmt.Table, opts); err != nil {
			return err
		}
		return nil
	case partiql.UpdateStmt:
		return validateUpdate(q, stmt, opts)
	case partiql.DeleteStmt:
		return validateDelete(q, stmt, opts)
	default:
		return fmt.Errorf("%s: unsupported statement", q.Name)
	}
}

func validateSelect(q Query, stmt partiql.SelectStmt, opts ValidateOptions) error {
	if q.Kind != KindOne && q.Kind != KindMany {
		return fmt.Errorf("%s: SELECT must be :one or :many", q.Name)
	}
	if err := checkTable(stmt.Table, opts.Table); err != nil {
		return err
	}
	if err := validateProjection(q); err != nil {
		return err
	}
	if err := validateSelectLimit(q, stmt); err != nil {
		return err
	}
	keyPK, keySK, err := resolveKeys(q, opts)
	if err != nil {
		return err
	}
	conds, err := condMap(stmt.Where)
	if err != nil {
		return fmt.Errorf("%s: %w", q.Name, err)
	}
	if err := validateKeyConditions(q, conds, keyPK, keySK); err != nil {
		return err
	}
	if q.Kind == KindOne {
		if err := validateOneSelect(q, conds, keyPK, keySK); err != nil {
			return err
		}
	}
	return nil
}

func validateProjection(q Query) error {
	if q.Annotations.Projection != "" && q.Annotations.Index == nil {
		return fmt.Errorf("%s: @projection requires @index", q.Name)
	}
	if q.Annotations.Index != nil && q.Annotations.Projection != "all" {
		return fmt.Errorf("%s: @index requires @projection all", q.Name)
	}
	if q.Annotations.Projection != "" && q.Annotations.Projection != "all" {
		return fmt.Errorf("%s: only @projection all is supported", q.Name)
	}
	return nil
}

func validateSelectLimit(q Query, stmt partiql.SelectStmt) error {
	if q.Kind == KindMany {
		if stmt.Limit != nil && q.Annotations.Limit != nil {
			return fmt.Errorf("%s: use either LIMIT or @limit", q.Name)
		}
		if q.Annotations.NoLimit && (stmt.Limit != nil || q.Annotations.Limit != nil) {
			return fmt.Errorf("%s: @nolimit conflicts with limit", q.Name)
		}
		if !q.Annotations.NoLimit && stmt.Limit == nil && q.Annotations.Limit == nil {
			return fmt.Errorf("%s: :many requires LIMIT/@limit or @nolimit", q.Name)
		}
		if stmt.Limit != nil && *stmt.Limit <= 0 {
			return fmt.Errorf("%s: LIMIT must be positive", q.Name)
		}
		return nil
	}
	if stmt.Limit != nil || q.Annotations.Limit != nil || q.Annotations.NoLimit {
		return fmt.Errorf("%s: limit annotations only valid for :many", q.Name)
	}
	return nil
}

func ensureNoSelectAnnotations(q Query) error {
	if q.Annotations.Limit != nil {
		return fmt.Errorf("%s: @limit only valid for SELECT", q.Name)
	}
	if q.Annotations.NoLimit {
		return fmt.Errorf("%s: @nolimit only valid for SELECT", q.Name)
	}
	if q.Annotations.Index != nil {
		return fmt.Errorf("%s: @index only valid for SELECT", q.Name)
	}
	if q.Annotations.Projection != "" {
		return fmt.Errorf("%s: @projection only valid for SELECT", q.Name)
	}
	return nil
}

func validateUpdate(q Query, stmt partiql.UpdateStmt, opts ValidateOptions) error {
	if err := validateWriteBase(q, stmt.Table, opts); err != nil {
		return err
	}
	if q.Kind == KindExec && stmt.Returning != nil {
		return fmt.Errorf("%s: :exec UPDATE cannot RETURNING", q.Name)
	}
	if q.Kind == KindOne && stmt.Returning == nil {
		return fmt.Errorf("%s: :one UPDATE requires RETURNING", q.Name)
	}
	if q.Kind == KindOne && stmt.Returning != nil {
		if stmt.Returning.Mode != partiql.ReturnAllOld && stmt.Returning.Mode != partiql.ReturnAllNew {
			return fmt.Errorf("%s: :one UPDATE requires RETURNING ALL OLD/NEW", q.Name)
		}
	}
	if q.Kind != KindExec && q.Kind != KindOne {
		return fmt.Errorf("%s: UPDATE must be :exec or :one", q.Name)
	}
	pk, sk, err := resolveKeys(q, opts)
	if err != nil {
		return err
	}
	if err := validateSetClauses(q, stmt.Set, pk, sk); err != nil {
		return err
	}
	conds, err := condMap(stmt.Where)
	if err != nil {
		return fmt.Errorf("%s: %w", q.Name, err)
	}
	if err := validateKeyConditionsExact(q, conds, pk, sk); err != nil {
		return err
	}
	return nil
}

func validateDelete(q Query, stmt partiql.DeleteStmt, opts ValidateOptions) error {
	if err := validateWriteBase(q, stmt.Table, opts); err != nil {
		return err
	}
	if q.Kind == KindExec && stmt.Returning != nil {
		return fmt.Errorf("%s: :exec DELETE cannot RETURNING", q.Name)
	}
	if q.Kind == KindOne {
		if stmt.Returning == nil {
			return fmt.Errorf("%s: :one DELETE requires RETURNING", q.Name)
		}
		if stmt.Returning.Mode != partiql.ReturnAllOld {
			return fmt.Errorf("%s: DELETE :one only supports RETURNING ALL OLD", q.Name)
		}
	} else if q.Kind != KindExec {
		return fmt.Errorf("%s: DELETE must be :exec or :one", q.Name)
	}
	pk, sk, err := resolveKeys(q, opts)
	if err != nil {
		return err
	}
	conds, err := condMap(stmt.Where)
	if err != nil {
		return fmt.Errorf("%s: %w", q.Name, err)
	}
	if err := validateKeyConditionsExact(q, conds, pk, sk); err != nil {
		return err
	}
	return nil
}

func validateWriteBase(q Query, table string, opts ValidateOptions) error {
	if err := ensureNoSelectAnnotations(q); err != nil {
		return err
	}
	return checkTable(table, opts.Table)
}

func validateSetClauses(q Query, clauses []partiql.SetClause, pk, sk []string) error {
	if len(clauses) == 0 {
		return fmt.Errorf("%s: UPDATE requires SET", q.Name)
	}
	keyAttrs := map[string]bool{}
	for _, attr := range pk {
		keyAttrs[attr] = true
	}
	for _, attr := range sk {
		keyAttrs[attr] = true
	}
	seen := map[string]bool{}
	for _, clause := range clauses {
		if keyAttrs[clause.Attr] {
			return fmt.Errorf("%s: UPDATE cannot SET key attribute %s", q.Name, clause.Attr)
		}
		if seen[clause.Attr] {
			return fmt.Errorf("%s: duplicate SET attribute %s", q.Name, clause.Attr)
		}
		seen[clause.Attr] = true
	}
	return nil
}

func checkTable(queryTable, expected string) error {
	if expected == "" {
		return nil
	}
	if !strings.EqualFold(queryTable, expected) {
		return fmt.Errorf("table mismatch: query uses %s, flag uses %s", queryTable, expected)
	}
	return nil
}

func resolveKeys(q Query, opts ValidateOptions) ([]string, []string, error) {
	if q.Annotations.Index != nil {
		idx := q.Annotations.Index
		if len(idx.PK) == 0 {
			return nil, nil, fmt.Errorf("%s: @index requires pk", q.Name)
		}
		if len(idx.PK) > 4 || len(idx.SK) > 4 {
			return nil, nil, fmt.Errorf("%s: @index supports up to 4 pk/sk attrs", q.Name)
		}
		pkNames := make([]string, 0, len(idx.PK))
		skNames := make([]string, 0, len(idx.SK))
		seen := map[string]bool{}
		for _, attr := range idx.PK {
			if seen[attr.Name] {
				return nil, nil, fmt.Errorf("%s: duplicate index key %s", q.Name, attr.Name)
			}
			seen[attr.Name] = true
			pkNames = append(pkNames, attr.Name)
		}
		for _, attr := range idx.SK {
			if seen[attr.Name] {
				return nil, nil, fmt.Errorf("%s: duplicate index key %s", q.Name, attr.Name)
			}
			seen[attr.Name] = true
			skNames = append(skNames, attr.Name)
		}
		return pkNames, skNames, nil
	}
	if opts.PK == "" {
		return nil, nil, fmt.Errorf("%s: pk is required", q.Name)
	}
	pk := []string{opts.PK}
	var sk []string
	if opts.SK != "" {
		sk = []string{opts.SK}
	}
	return pk, sk, nil
}

func condMap(conds []partiql.Cond) (map[string]partiql.Cond, error) {
	m := make(map[string]partiql.Cond)
	for _, cond := range conds {
		if _, ok := m[cond.Attr]; ok {
			return nil, fmt.Errorf("duplicate condition on %s", cond.Attr)
		}
		m[cond.Attr] = cond
	}
	return m, nil
}

func validateKeyConditions(q Query, conds map[string]partiql.Cond, pk, sk []string) error {
	for attr, cond := range conds {
		if !contains(pk, attr) && !contains(sk, attr) {
			return fmt.Errorf("%s: non-key attribute in WHERE: %s", q.Name, attr)
		}
		if cond.Type == partiql.CondBetween || cond.Type == partiql.CondBeginsWith {
			if !contains(sk, attr) {
				return fmt.Errorf("%s: begins_with/BETWEEN only allowed on SK", q.Name)
			}
		}
		if cond.Type != partiql.CondEq && cond.Type != partiql.CondBeginsWith && cond.Type != partiql.CondBetween {
			return fmt.Errorf("%s: unsupported condition", q.Name)
		}
	}
	for _, attr := range pk {
		cond, ok := conds[attr]
		if !ok {
			return fmt.Errorf("%s: missing PK condition %s", q.Name, attr)
		}
		if cond.Type != partiql.CondEq {
			return fmt.Errorf("%s: PK condition must be =", q.Name)
		}
	}
	if len(sk) == 0 {
		return nil
	}
	for i, attr := range sk {
		cond, ok := conds[attr]
		if !ok {
			if hasLaterCond(conds, sk[i+1:]) {
				return fmt.Errorf("%s: SK condition must be left-contiguous", q.Name)
			}
			return nil
		}
		switch cond.Type {
		case partiql.CondEq:
			continue
		case partiql.CondBeginsWith, partiql.CondBetween:
			if hasLaterCond(conds, sk[i+1:]) {
				return fmt.Errorf("%s: begins_with/BETWEEN must be last SK condition", q.Name)
			}
			return nil
		default:
			return fmt.Errorf("%s: unsupported SK condition", q.Name)
		}
	}
	return nil
}

func validateOneSelect(q Query, conds map[string]partiql.Cond, pk, sk []string) error {
	for _, attr := range pk {
		cond := conds[attr]
		if cond.Type != partiql.CondEq {
			return fmt.Errorf("%s: :one requires PK equality", q.Name)
		}
	}
	for _, attr := range sk {
		cond, ok := conds[attr]
		if !ok {
			return fmt.Errorf("%s: :one requires full key equality", q.Name)
		}
		if cond.Type != partiql.CondEq {
			return fmt.Errorf("%s: :one requires full key equality", q.Name)
		}
	}
	for attr, cond := range conds {
		if contains(pk, attr) || contains(sk, attr) {
			if cond.Type != partiql.CondEq {
				return fmt.Errorf("%s: :one only supports = conditions", q.Name)
			}
			continue
		}
		return fmt.Errorf("%s: non-key attribute in WHERE: %s", q.Name, attr)
	}
	return nil
}

func validateKeyConditionsExact(q Query, conds map[string]partiql.Cond, pk, sk []string) error {
	for attr := range conds {
		if !contains(pk, attr) && !contains(sk, attr) {
			return fmt.Errorf("%s: non-key attribute in WHERE: %s", q.Name, attr)
		}
	}
	for _, attr := range pk {
		cond, ok := conds[attr]
		if !ok || cond.Type != partiql.CondEq {
			return fmt.Errorf("%s: missing PK equality %s", q.Name, attr)
		}
	}
	for _, attr := range sk {
		cond, ok := conds[attr]
		if !ok || cond.Type != partiql.CondEq {
			return fmt.Errorf("%s: missing SK equality %s", q.Name, attr)
		}
	}
	return nil
}

func contains(list []string, s string) bool {
	return slices.Contains(list, s)
}

func hasLaterCond(conds map[string]partiql.Cond, later []string) bool {
	for _, attr := range later {
		if _, ok := conds[attr]; ok {
			return true
		}
	}
	return false
}
