package model

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/printer"
	"go/token"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
)

type Registry struct {
	Types   map[string]TypeInfo
	Imports map[string]string
}

type TypeInfo struct {
	Name   string
	Fields map[string]FieldInfo
}

type FieldInfo struct {
	GoType string
}

func Parse(paths []string) (*Registry, error) {
	reg := &Registry{Types: map[string]TypeInfo{}, Imports: map[string]string{}}
	for _, path := range paths {
		files, err := collectGoFiles(path)
		if err != nil {
			return nil, err
		}
		for _, file := range files {
			if err := parseFile(reg, file); err != nil {
				return nil, err
			}
		}
	}
	return reg, nil
}

func collectGoFiles(path string) ([]string, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		if strings.HasSuffix(path, ".go") && !strings.HasSuffix(path, "_test.go") {
			return []string{path}, nil
		}
		return nil, fmt.Errorf("model path must be .go file or directory: %s", path)
	}
	entries, err := os.ReadDir(path)
	if err != nil {
		return nil, err
	}
	var files []string
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if strings.HasSuffix(name, ".go") && !strings.HasSuffix(name, "_test.go") {
			files = append(files, filepath.Join(path, name))
		}
	}
	if len(files) == 0 {
		return nil, fmt.Errorf("no .go files in %s", path)
	}
	return files, nil
}

func parseFile(reg *Registry, path string) error {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, path, nil, parser.ParseComments)
	if err != nil {
		return err
	}
	for _, imp := range file.Imports {
		impPath, err := strconv.Unquote(imp.Path.Value)
		if err != nil {
			return err
		}
		alias := ""
		if imp.Name != nil {
			alias = imp.Name.Name
		} else {
			alias = filepath.Base(impPath)
		}
		if alias == "_" || alias == "." {
			continue
		}
		if prev, ok := reg.Imports[alias]; ok && prev != impPath {
			return fmt.Errorf("import alias conflict for %s: %s vs %s", alias, prev, impPath)
		}
		reg.Imports[alias] = impPath
	}
	for _, decl := range file.Decls {
		gen, ok := decl.(*ast.GenDecl)
		if !ok || gen.Tok != token.TYPE {
			continue
		}
		for _, spec := range gen.Specs {
			ts := spec.(*ast.TypeSpec)
			st, ok := ts.Type.(*ast.StructType)
			if !ok {
				continue
			}
			fields := map[string]FieldInfo{}
			for _, field := range st.Fields.List {
				if field.Tag == nil {
					continue
				}
				if len(field.Names) == 0 {
					continue
				}
				tagVal, err := strconv.Unquote(field.Tag.Value)
				if err != nil {
					return err
				}
				tag := reflect.StructTag(tagVal)
				dyn := tag.Get("dynamodbav")
				if dyn == "" || dyn == "-" {
					continue
				}
				attr := strings.Split(dyn, ",")[0]
				if attr == "" {
					continue
				}
				if _, ok := fields[attr]; ok {
					return fmt.Errorf("%s: duplicate attribute %s", ts.Name.Name, attr)
				}
				fields[attr] = FieldInfo{GoType: exprString(fset, field.Type)}
			}
			if len(fields) > 0 {
				reg.Types[ts.Name.Name] = TypeInfo{Name: ts.Name.Name, Fields: fields}
			}
		}
	}
	return nil
}

func exprString(fset *token.FileSet, expr ast.Expr) string {
	var sb strings.Builder
	_ = printer.Fprint(&sb, fset, expr)
	return sb.String()
}

func (r *Registry) FieldType(modelName, attr string) (string, bool) {
	model, ok := r.Types[modelName]
	if !ok {
		return "", false
	}
	field, ok := model.Fields[attr]
	if !ok {
		return "", false
	}
	return field.GoType, true
}

func (r *Registry) HasModel(name string) bool {
	_, ok := r.Types[name]
	return ok
}
