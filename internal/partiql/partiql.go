package partiql

import (
	"fmt"
	"strconv"
	"strings"
	"unicode"
)

type Statement interface {
	stmtKind() string
}

type SelectStmt struct {
	Table string
	Where []Cond
	Limit *int
}

func (SelectStmt) stmtKind() string { return "select" }

type InsertStmt struct {
	Table string
}

func (InsertStmt) stmtKind() string { return "insert" }

type UpdateStmt struct {
	Table     string
	Set       []SetClause
	Where     []Cond
	Returning *Returning
}

func (UpdateStmt) stmtKind() string { return "update" }

type DeleteStmt struct {
	Table     string
	Where     []Cond
	Returning *Returning
}

func (DeleteStmt) stmtKind() string { return "delete" }

type CondType int

const (
	CondEq CondType = iota
	CondBeginsWith
	CondBetween
)

type Cond struct {
	Type CondType
	Attr string
}

type SetClause struct {
	Attr string
}

type ReturnMode int

const (
	ReturnAllOld ReturnMode = iota
	ReturnAllNew
	ReturnUpdatedOld
	ReturnUpdatedNew
)

type Returning struct {
	Mode ReturnMode
}

type tokenKind int

const (
	tokIdent tokenKind = iota
	tokNumber
	tokSymbol
	tokOperator
	tokPlaceholder
	tokEOF
)

type token struct {
	kind  tokenKind
	value string
	pos   int
}

func Parse(sql string) (Statement, error) {
	l := lexer{src: sql}
	toks, err := l.lex()
	if err != nil {
		return nil, err
	}
	p := parser{tokens: toks}
	return p.parseStatement()
}

type lexer struct {
	src string
	pos int
}

func (l *lexer) lex() ([]token, error) {
	var toks []token
	for {
		l.skipWS()
		if l.pos >= len(l.src) {
			toks = append(toks, token{kind: tokEOF, pos: l.pos})
			return toks, nil
		}
		ch := l.src[l.pos]
		switch ch {
		case '?':
			toks = append(toks, token{kind: tokPlaceholder, value: "?", pos: l.pos})
			l.pos++
		case '"':
			val, pos, err := l.readQuotedIdent()
			if err != nil {
				return nil, err
			}
			toks = append(toks, token{kind: tokIdent, value: val, pos: pos})
		case '(', ')', ',', '*':
			toks = append(toks, token{kind: tokSymbol, value: string(ch), pos: l.pos})
			l.pos++
		case '=', '<', '>', '!':
			toks = append(toks, l.readOperator())
		default:
			r := rune(ch)
			switch {
			case isIdentStart(r):
				start := l.pos
				val := l.readWhile(isIdentPart)
				toks = append(toks, token{kind: tokIdent, value: val, pos: start})
			case unicode.IsDigit(r):
				start := l.pos
				val := l.readWhile(unicode.IsDigit)
				toks = append(toks, token{kind: tokNumber, value: val, pos: start})
			default:
				return nil, fmt.Errorf("unexpected character: %q", ch)
			}
		}
	}
}

func (l *lexer) skipWS() {
	for l.pos < len(l.src) {
		if unicode.IsSpace(rune(l.src[l.pos])) {
			l.pos++
			continue
		}
		return
	}
}

func (l *lexer) readWhile(pred func(rune) bool) string {
	start := l.pos
	for l.pos < len(l.src) {
		if !pred(rune(l.src[l.pos])) {
			break
		}
		l.pos++
	}
	return l.src[start:l.pos]
}

func (l *lexer) readQuotedIdent() (string, int, error) {
	start := l.pos
	l.pos++
	for l.pos < len(l.src) && l.src[l.pos] != '"' {
		l.pos++
	}
	if l.pos >= len(l.src) {
		return "", 0, fmt.Errorf("unterminated quoted identifier")
	}
	val := l.src[start+1 : l.pos]
	l.pos++
	return val, start, nil
}

func (l *lexer) readOperator() token {
	start := l.pos
	l.pos++
	if l.pos < len(l.src) && (l.src[l.pos] == '=' || (l.src[start] == '<' && l.src[l.pos] == '>')) {
		l.pos++
	}
	return token{kind: tokOperator, value: l.src[start:l.pos], pos: start}
}

func isIdentStart(r rune) bool {
	return r == '_' || unicode.IsLetter(r)
}

func isIdentPart(r rune) bool {
	return r == '_' || unicode.IsLetter(r) || unicode.IsDigit(r)
}

type parser struct {
	tokens []token
	pos    int
}

func (p *parser) parseStatement() (Statement, error) {
	if p.consumeKeyword("SELECT") {
		return p.parseSelect()
	}
	if p.consumeKeyword("INSERT") {
		return p.parseInsert()
	}
	if p.consumeKeyword("UPDATE") {
		return p.parseUpdate()
	}
	if p.consumeKeyword("DELETE") {
		return p.parseDelete()
	}
	return nil, p.errorAt("expected SELECT/INSERT/UPDATE/DELETE")
}

func (p *parser) parseSelect() (Statement, error) {
	if !p.consumeSymbol("*") {
		return nil, p.errorAt("only SELECT * is supported")
	}
	if !p.consumeKeyword("FROM") {
		return nil, p.errorAt("expected FROM")
	}
	table, err := p.parseIdent()
	if err != nil {
		return nil, err
	}
	if !p.consumeKeyword("WHERE") {
		return nil, p.errorAt("expected WHERE")
	}
	conds, err := p.parseWhere()
	if err != nil {
		return nil, err
	}
	var limit *int
	if p.consumeKeyword("LIMIT") {
		n, err := p.parseNumber()
		if err != nil {
			return nil, err
		}
		limit = &n
	}
	if err := p.finishStatement("SELECT"); err != nil {
		return nil, err
	}
	return SelectStmt{Table: table, Where: conds, Limit: limit}, nil
}

func (p *parser) parseInsert() (Statement, error) {
	if !p.consumeKeyword("INTO") {
		return nil, p.errorAt("expected INTO")
	}
	table, err := p.parseIdent()
	if err != nil {
		return nil, err
	}
	if !p.consumeKeyword("VALUE") {
		return nil, p.errorAt("expected VALUE")
	}
	if !p.consumePlaceholder() {
		return nil, p.errorAt("expected ? placeholder")
	}
	if err := p.finishStatement("INSERT"); err != nil {
		return nil, err
	}
	return InsertStmt{Table: table}, nil
}

func (p *parser) parseUpdate() (Statement, error) {
	table, err := p.parseIdent()
	if err != nil {
		return nil, err
	}
	if !p.consumeKeyword("SET") {
		return nil, p.errorAt("expected SET")
	}
	setClauses, err := p.parseSetClauses()
	if err != nil {
		return nil, err
	}
	if !p.consumeKeyword("WHERE") {
		return nil, p.errorAt("expected WHERE")
	}
	conds, err := p.parseWhere()
	if err != nil {
		return nil, err
	}
	ret, err := p.parseOptionalReturning()
	if err != nil {
		return nil, err
	}
	if err := p.finishStatement("UPDATE"); err != nil {
		return nil, err
	}
	return UpdateStmt{Table: table, Set: setClauses, Where: conds, Returning: ret}, nil
}

func (p *parser) parseDelete() (Statement, error) {
	if !p.consumeKeyword("FROM") {
		return nil, p.errorAt("expected FROM")
	}
	table, err := p.parseIdent()
	if err != nil {
		return nil, err
	}
	if !p.consumeKeyword("WHERE") {
		return nil, p.errorAt("expected WHERE")
	}
	conds, err := p.parseWhere()
	if err != nil {
		return nil, err
	}
	ret, err := p.parseOptionalReturning()
	if err != nil {
		return nil, err
	}
	if err := p.finishStatement("DELETE"); err != nil {
		return nil, err
	}
	return DeleteStmt{Table: table, Where: conds, Returning: ret}, nil
}

func (p *parser) parseSetClauses() ([]SetClause, error) {
	var clauses []SetClause
	for {
		attr, err := p.parseIdent()
		if err != nil {
			return nil, err
		}
		if !p.consumeOperator("=") {
			return nil, p.errorAt("expected = in SET clause")
		}
		if !p.consumePlaceholder() {
			return nil, p.errorAt("expected ? placeholder in SET")
		}
		clauses = append(clauses, SetClause{Attr: attr})
		if p.consumeSymbol(",") {
			continue
		}
		return clauses, nil
	}
}

func (p *parser) parseWhere() ([]Cond, error) {
	var conds []Cond
	for {
		cond, err := p.parseCond()
		if err != nil {
			return nil, err
		}
		conds = append(conds, cond)
		if p.consumeKeyword("AND") {
			continue
		}
		if p.consumeKeyword("OR") || p.consumeKeyword("NOT") {
			return nil, p.errorAt("OR/NOT is not supported")
		}
		break
	}
	return conds, nil
}

func (p *parser) parseCond() (Cond, error) {
	if p.consumeKeyword("begins_with") {
		return p.parseFuncCond(CondBeginsWith)
	}
	attr, err := p.parseIdent()
	if err != nil {
		return Cond{}, err
	}
	if p.consumeKeyword("IN") {
		return Cond{}, p.errorAt("IN is not supported")
	}
	if p.consumeKeyword("BETWEEN") {
		if !p.consumePlaceholder() {
			return Cond{}, p.errorAt("expected ? placeholder")
		}
		if !p.consumeKeyword("AND") {
			return Cond{}, p.errorAt("expected AND in BETWEEN")
		}
		if !p.consumePlaceholder() {
			return Cond{}, p.errorAt("expected ? placeholder")
		}
		return Cond{Type: CondBetween, Attr: attr}, nil
	}
	if p.consumeOperator("=") {
		if !p.consumePlaceholder() {
			return Cond{}, p.errorAt("expected ? placeholder")
		}
		return Cond{Type: CondEq, Attr: attr}, nil
	}
	if p.peekOperator() {
		return Cond{}, p.errorAt("unsupported operator")
	}
	return Cond{}, p.errorAt("expected condition")
}

func (p *parser) parseFuncCond(condType CondType) (Cond, error) {
	if !p.consumeSymbol("(") {
		return Cond{}, p.errorAt("expected (")
	}
	attr, err := p.parseIdent()
	if err != nil {
		return Cond{}, err
	}
	if !p.consumeSymbol(",") {
		return Cond{}, p.errorAt("expected ,")
	}
	if !p.consumePlaceholder() {
		return Cond{}, p.errorAt("expected ? placeholder")
	}
	if !p.consumeSymbol(")") {
		return Cond{}, p.errorAt("expected )")
	}
	return Cond{Type: condType, Attr: attr}, nil
}

func (p *parser) parseReturning() (*Returning, error) {
	modeAll := p.consumeKeyword("ALL")
	modeUpdated := p.consumeKeyword("UPDATED")
	if !modeAll && !modeUpdated {
		return nil, p.errorAt("expected ALL or UPDATED")
	}
	if p.consumeKeyword("OLD") {
		p.consumeSymbol("*")
		if modeAll {
			return &Returning{Mode: ReturnAllOld}, nil
		}
		return &Returning{Mode: ReturnUpdatedOld}, nil
	}
	if p.consumeKeyword("NEW") {
		p.consumeSymbol("*")
		if modeAll {
			return &Returning{Mode: ReturnAllNew}, nil
		}
		return &Returning{Mode: ReturnUpdatedNew}, nil
	}
	return nil, p.errorAt("expected OLD or NEW")
}

func (p *parser) parseOptionalReturning() (*Returning, error) {
	if !p.consumeKeyword("RETURNING") {
		return nil, nil
	}
	return p.parseReturning()
}

func (p *parser) finishStatement(kind string) error {
	if p.atEnd() {
		return nil
	}
	return p.errorAt("unexpected tokens after " + kind)
}

func (p *parser) parseIdent() (string, error) {
	if p.atEnd() {
		return "", p.errorAt("expected identifier")
	}
	tok := p.tokens[p.pos]
	if tok.kind != tokIdent {
		return "", p.errorAt("expected identifier")
	}
	p.pos++
	return tok.value, nil
}

func (p *parser) parseNumber() (int, error) {
	if p.atEnd() {
		return 0, p.errorAt("expected number")
	}
	tok := p.tokens[p.pos]
	if tok.kind != tokNumber {
		return 0, p.errorAt("expected number")
	}
	p.pos++
	val, err := strconv.Atoi(tok.value)
	if err != nil {
		return 0, p.errorAt("invalid number")
	}
	return val, nil
}

func (p *parser) consumeKeyword(word string) bool {
	if p.matchKeyword(word) {
		p.pos++
		return true
	}
	return false
}

func (p *parser) matchKeyword(word string) bool {
	if p.atEnd() {
		return false
	}
	tok := p.tokens[p.pos]
	if tok.kind != tokIdent {
		return false
	}
	return strings.EqualFold(tok.value, word)
}

func (p *parser) consumeSymbol(sym string) bool {
	if p.atEnd() {
		return false
	}
	tok := p.tokens[p.pos]
	if tok.kind == tokSymbol && tok.value == sym {
		p.pos++
		return true
	}
	return false
}

func (p *parser) consumeOperator(op string) bool {
	if p.atEnd() {
		return false
	}
	tok := p.tokens[p.pos]
	if tok.kind == tokOperator && tok.value == op {
		p.pos++
		return true
	}
	return false
}

func (p *parser) consumePlaceholder() bool {
	if p.atEnd() {
		return false
	}
	tok := p.tokens[p.pos]
	if tok.kind == tokPlaceholder {
		p.pos++
		return true
	}
	return false
}

func (p *parser) peekOperator() bool {
	if p.atEnd() {
		return false
	}
	return p.tokens[p.pos].kind == tokOperator
}

func (p *parser) atEnd() bool {
	return p.tokens[p.pos].kind == tokEOF
}

func (p *parser) errorAt(msg string) error {
	if p.atEnd() {
		return fmt.Errorf("%s at end", msg)
	}
	tok := p.tokens[p.pos]
	return fmt.Errorf("%s near %q", msg, tok.value)
}
