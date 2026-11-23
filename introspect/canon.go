package introspect

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/xbmlz/mist/schema"
)

// typeFact is the type facts for one column in pg_catalog.
type typeFact struct {
	Name        string
	TypeKind    string
	Category    string
	Mod         int
	Dims        int
	TableSchema string
	TypeSchema  string
	ElemName    string
	ElemKind    string
	ElemSchema  string
}

var serialDefault = regexp.MustCompile(`^nextval\('.+'::regclass\)$`)

var literalCasts = map[string]struct{}{
	"text":                        {},
	"character varying":           {},
	"integer":                     {},
	"bigint":                      {},
	"smallint":                    {},
	"boolean":                     {},
	"numeric":                     {},
	"uuid":                        {},
	"timestamp with time zone":    {},
	"timestamp without time zone": {},
	"double precision":            {},
	"real":                        {},
}

// canonicalType folds a catalog type into schema.Type.
func canonicalType(fact typeFact, column string) (schema.Type, error) {
	if fact.Category == "A" {
		if fact.ElemKind != "b" && fact.ElemKind != "e" {
			return schema.Type{}, fmt.Errorf("introspect: array element type of column %s is not supported yet", column)
		}
		if foreignSchema(fact.ElemSchema, fact.TableSchema) {
			return schema.Type{}, fmt.Errorf("introspect: column %s type lives in schema %s; other schemas are not supported yet", column, fact.ElemSchema)
		}
		dims := fact.Dims
		if dims == 0 {
			dims = 1
		}
		if fact.ElemKind == "e" {
			return schema.Type{Name: fact.ElemName, Enum: true, ArrayDims: dims}, nil
		}
		elem := typeFact{Name: fact.ElemName, TypeKind: "b", Mod: fact.Mod, TableSchema: fact.TableSchema, TypeSchema: fact.ElemSchema}
		base, err := canonicalType(elem, column)
		if err != nil {
			return schema.Type{}, err
		}
		base.ArrayDims = dims
		return base, nil
	}
	switch fact.TypeKind {
	case "e":
		if foreignSchema(fact.TypeSchema, fact.TableSchema) {
			return schema.Type{}, fmt.Errorf("introspect: column %s type lives in schema %s; other schemas are not supported yet", column, fact.TypeSchema)
		}
		return schema.Type{Name: fact.Name, Enum: true}, nil
	case "b":
		return builtinType(fact), nil
	default:
		return schema.Type{}, fmt.Errorf("introspect: column %s type %s is not supported yet", column, fact.Name)
	}
}

func foreignSchema(typeSchema, tableSchema string) bool {
	return typeSchema != "" && typeSchema != "pg_catalog" && typeSchema != tableSchema
}

func builtinType(fact typeFact) schema.Type {
	switch fact.Name {
	case "int8":
		return schema.Type{Name: "bigint"}
	case "int4":
		return schema.Type{Name: "integer"}
	case "int2":
		return schema.Type{Name: "smallint"}
	case "bool":
		return schema.Type{Name: "boolean"}
	case "varchar":
		typ := schema.Type{Name: "varchar"}
		if fact.Mod > 4 {
			typ.Length = fact.Mod - 4
		}
		return typ
	case "numeric":
		typ := schema.Type{Name: "numeric"}
		if fact.Mod > 4 {
			mod := fact.Mod - 4
			typ.Precision = (mod >> 16) & 0xffff
			typ.Scale = mod & 0xffff
		}
		return typ
	default:
		return schema.Type{Name: fact.Name}
	}
}

func foldColumn(col *schema.Column) {
	if col.Generated != "" {
		col.Generated = unwrapExpr(col.Generated)
		col.Default = ""
		return
	}
	if col.Identity {
		col.Default = ""
		return
	}
	col.Default = normalizeDefault(col.Default)
	if !serialDefault.MatchString(col.Default) {
		return
	}
	switch col.Type.Name {
	case "bigint":
		col.Type.Name = "bigserial"
		col.Default = ""
	case "integer":
		col.Type.Name = "serial"
		col.Default = ""
	case "smallint":
		col.Type.Name = "smallserial"
		col.Default = ""
	}
}

func normalizeDefault(expr string) string {
	expr = strings.TrimSpace(expr)
	i := strings.LastIndex(expr, "::")
	if i <= 0 {
		return expr
	}
	left := strings.TrimSpace(expr[:i])
	right := strings.TrimSpace(expr[i+2:])
	if isSimpleLiteral(left) {
		if _, ok := literalCasts[right]; ok {
			return left
		}
	}
	return expr
}

func isSimpleLiteral(s string) bool {
	switch s {
	case "true", "false", "null":
		return true
	}
	if len(s) >= 2 && s[0] == '\'' && s[len(s)-1] == '\'' {
		return true
	}
	if s == "" || s == "+" || s == "-" || s == "." {
		return false
	}
	for _, r := range s {
		if r != '+' && r != '-' && r != '.' && (r < '0' || r > '9') {
			return false
		}
	}
	return true
}

func unwrapExpr(expr string) string {
	expr = strings.TrimSpace(expr)
	for wrapped(expr) {
		expr = strings.TrimSpace(expr[1 : len(expr)-1])
	}
	return expr
}

func wrapped(expr string) bool {
	if len(expr) < 2 || expr[0] != '(' || expr[len(expr)-1] != ')' {
		return false
	}
	depth := 0
	for i := 0; i < len(expr); i++ {
		switch expr[i] {
		case '(':
			depth++
		case ')':
			depth--
			if depth == 0 && i != len(expr)-1 {
				return false
			}
		}
	}
	return depth == 0
}

func fkAction(code string) (string, error) {
	switch code {
	case "", "a":
		return "", nil
	case "r":
		return "restrict", nil
	case "c":
		return "cascade", nil
	case "n":
		return "set null", nil
	case "d":
		return "set default", nil
	default:
		return "", fmt.Errorf("introspect: unknown foreign-key action %q", code)
	}
}
