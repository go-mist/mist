// Package codegen reads mist tags on structs and generates column descriptors and scan functions.
// Generation may inspect the syntax tree. Running a query does not read tags and does not reflect on fields.
package codegen

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
)

// Generate writes schema_gen.go in dir. On failure it leaves an existing file unchanged.
func Generate(dir string) error {
	src, err := GenerateSource(dir)
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, outFile), src, 0o644)
}

// GenerateSource returns the source that would be written.
func GenerateSource(dir string) ([]byte, error) {
	g, err := load(dir)
	if err != nil {
		return nil, err
	}
	if err := g.validate(); err != nil {
		return nil, err
	}
	return emit(g.pkg, g.tables)
}

const outFile = "schema_gen.go"

// shortcut: a tag is split on commas. When a default or generated expression contains a comma, wrap that segment in double quotes.
// shortcut: references accepts only table.column, not schema.table.column.
// shortcut: an exported name only appends s to the type name (User → Users). Irregular plurals are specified later by Table().
// shortcut: only embedded structs in this package are expanded. A cross-package embed or a generic is an error.

var sqlToGo = map[string]string{
	"bigserial":   "int64",
	"bigint":      "int64",
	"int8":        "int64",
	"serial":      "int32",
	"integer":     "int32",
	"int4":        "int32",
	"int":         "int32",
	"smallserial": "int16",
	"smallint":    "int16",
	"int2":        "int16",
	"text":        "string",
	"varchar":     "string",
	"uuid":        "string",
	"boolean":     "bool",
	"bool":        "bool",
	"bytea":       "[]byte",
	"timestamptz": "time.Time",
	"timestamp":   "time.Time",
	"date":        "time.Time",
	"time":        "time.Time",
	"jsonb":       "json.RawMessage",
	"json":        "json.RawMessage",
	"numeric":     "string",
	"decimal":     "string",
	"interval":    "time.Duration",
	"enum":        "string",
}

var actions = map[string]struct{}{
	"cascade":     {},
	"restrict":    {},
	"set null":    {},
	"set default": {},
	"no action":   {},
}

var integerTypes = map[string]struct{}{
	"bigserial": {}, "bigint": {}, "int8": {},
	"serial": {}, "integer": {}, "int4": {}, "int": {},
	"smallserial": {}, "smallint": {}, "int2": {},
}

var timeTypes = map[string]bool{
	"timestamptz": true,
	"timestamp":   true,
	"date":        true,
	"time":        true,
}

type table struct {
	pos      token.Pos
	file     string
	line     int
	typeName string
	varName  string
	sqlName  string
	dialect  string
	cols     []column
	primary  []string
	indexes  []indexSpec
	checks   []checkSpec
}

type indexSpec struct {
	line    int
	name    string
	columns []string
	unique  bool
}

type checkSpec struct {
	line int
	name string
	expr string
}

type column struct {
	line       int
	file       string
	field      string
	path       string
	goType     string
	colName    string
	sqlType    string
	notNull    bool
	pk         bool
	unique     bool
	uniqueName string
	def        string
	refTable   string
	refCol     string
	onDelete   string
	onUpdate   string
	length     int
	precision  int
	scale      int
	generated  string
	identity   bool
	enumName   string
}

type loader struct {
	dir    string
	fset   *token.FileSet
	pkg    string
	tables []table
	errs   []error
}

func load(dir string) (*loader, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	g := &loader{dir: dir, fset: token.NewFileSet()}
	structs := map[string]*astStruct{}
	methods := map[string]method{}
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") || name == outFile {
			continue
		}
		path := filepath.Join(dir, name)
		file, err := parser.ParseFile(g.fset, path, nil, 0)
		if err != nil {
			return nil, err
		}
		if g.pkg == "" {
			g.pkg = file.Name.Name
		} else if file.Name.Name != g.pkg {
			return nil, fmt.Errorf("%s: package name is %s, other files in the directory use %s", name, file.Name.Name, g.pkg)
		}
		collect(file, structs, methods, g)
	}
	if g.pkg == "" {
		return nil, fmt.Errorf("%s: no Go files", dir)
	}
	for name, st := range structs {
		g.expand(name, st, structs, map[string]bool{})
	}
	for name, st := range structs {
		m, hasMethod := methods[name]
		if !hasMethod && !st.hasTag {
			continue
		}
		g.errs = append(g.errs, st.tableErrs...)
		if !hasMethod {
			if st.hasTag && !st.embedded {
				g.fail(st.file, st.line, "type %s has mist tags but no Table() method", name)
			}
			continue
		}
		if len(st.cols) == 0 {
			g.fail(m.file, m.line, "type %s has no columns", name)
			continue
		}
		g.tables = append(g.tables, table{
			pos:      st.pos,
			file:     st.file,
			line:     st.line,
			typeName: name,
			varName:  exportName(name),
			sqlName:  m.sqlName,
			dialect:  m.dialect,
			cols:     st.cols,
			primary:  m.primary,
			indexes:  m.indexes,
			checks:   m.checks,
		})
	}
	for name, m := range methods {
		if _, ok := structs[name]; !ok {
			g.fail(m.file, m.line, "Table() receiver %s is not a struct", name)
		}
	}
	sortTables(g.tables)
	if len(g.tables) == 0 && len(g.errs) == 0 {
		g.fail("", 0, "no struct with Table() was found")
	}
	return g, nil
}

type astStruct struct {
	pos       token.Pos
	file      string
	line      int
	node      *ast.StructType
	cols      []column
	hasTag    bool
	embedded  bool
	expanded  bool
	tableErrs []error
}

type method struct {
	file    string
	line    int
	sqlName string
	dialect string
	primary []string
	indexes []indexSpec
	checks  []checkSpec
}

func collect(file *ast.File, structs map[string]*astStruct, methods map[string]method, g *loader) {
	for _, decl := range file.Decls {
		switch d := decl.(type) {
		case *ast.GenDecl:
			if d.Tok != token.TYPE {
				continue
			}
			for _, spec := range d.Specs {
				ts := spec.(*ast.TypeSpec)
				st, ok := ts.Type.(*ast.StructType)
				if !ok {
					continue
				}
				fileName, line := g.at(ts.Pos())
				if prev, ok := structs[ts.Name.Name]; ok {
					g.fail(fileName, line, "type %s is defined more than once (already at %s:%d)", ts.Name.Name, prev.file, prev.line)
					continue
				}
				parsed := &astStruct{pos: ts.Pos(), file: fileName, line: line, node: st}
				if ts.TypeParams != nil && len(ts.TypeParams.List) > 0 {
					parsed.tableErrs = append(parsed.tableErrs, g.err(fileName, line, "type %s: generics are not supported", ts.Name.Name))
				}
				structs[ts.Name.Name] = parsed
			}
		case *ast.FuncDecl:
			if d.Name.Name != "Table" || d.Recv == nil {
				continue
			}
			recv, ok := recvType(d)
			if !ok {
				fileName, line := g.at(d.Pos())
				g.fail(fileName, line, "Table() receiver must be a named struct")
				continue
			}
			fileName, line := g.at(d.Pos())
			body, err := tableLiteral(d)
			if err != nil {
				g.fail(fileName, line, "%s.Table() %s", recv, err.Error())
			}
			if prev, ok := methods[recv]; ok {
				g.fail(fileName, line, "%s.Table() is defined more than once (already at %s:%d)", recv, prev.file, prev.line)
				continue
			}
			methods[recv] = method{
				file: fileName, line: line, sqlName: body.sqlName, dialect: body.dialect,
				primary: body.primary, indexes: body.indexes, checks: body.checks,
			}
		}
	}
}

func (g *loader) expand(name string, st *astStruct, structs map[string]*astStruct, stack map[string]bool) {
	if st.expanded {
		return
	}
	if stack[name] {
		st.tableErrs = append(st.tableErrs, g.err(st.file, st.line, "type %s embed cycle", name))
		st.expanded = true
		return
	}
	stack[name] = true
	defer delete(stack, name)
	if st.node == nil || st.node.Fields == nil {
		st.expanded = true
		return
	}
	seenCol := map[string]int{}
	seenField := map[string]int{}
	for _, f := range st.node.Fields.List {
		fileName, line := g.at(f.Pos())
		if len(f.Names) == 0 {
			g.embed(fileName, line, f.Type, st, structs, stack, seenCol, seenField)
			continue
		}
		if len(f.Names) != 1 {
			st.tableErrs = append(st.tableErrs, g.err(fileName, line, "a column can have only one field name"))
			continue
		}
		field := f.Names[0].Name
		raw := tagValue(f)
		if raw == "" {
			if ast.IsExported(field) {
				st.tableErrs = append(st.tableErrs, g.err(fileName, line, "exported field %s is missing a mist tag", field))
			}
			continue
		}
		st.hasTag = true
		if !ast.IsExported(field) {
			st.tableErrs = append(st.tableErrs, g.err(fileName, line, "field %s is unexported and cannot be a column", field))
			continue
		}
		if reservedField(field) {
			st.tableErrs = append(st.tableErrs, g.err(fileName, line, "field name %s collides with the generated table descriptor", field))
			continue
		}
		col, err := g.parseTag(fileName, line, field, f.Type, raw)
		if err != nil {
			st.tableErrs = append(st.tableErrs, err)
			continue
		}
		if prev, ok := seenCol[col.colName]; ok {
			st.tableErrs = append(st.tableErrs, g.err(fileName, line, "column name %s duplicates row %d", col.colName, prev))
			continue
		}
		if prev, ok := seenField[col.field]; ok {
			st.tableErrs = append(st.tableErrs, g.err(fileName, line, "field %s duplicates row %d", col.field, prev))
			continue
		}
		seenCol[col.colName] = line
		seenField[col.field] = line
		st.cols = append(st.cols, col)
	}
	st.expanded = true
}

func (g *loader) embed(fileName string, line int, expr ast.Expr, dst *astStruct, structs map[string]*astStruct, stack map[string]bool, seenCol, seenField map[string]int) {
	name, kind := embedName(expr)
	switch kind {
	case embedCross:
		dst.tableErrs = append(dst.tableErrs, g.err(fileName, line, "embedded fields from another package are not supported"))
	case embedGeneric:
		dst.tableErrs = append(dst.tableErrs, g.err(fileName, line, "generic embedded fields are not supported"))
	case embedBad:
		dst.tableErrs = append(dst.tableErrs, g.err(fileName, line, "embedded fields are not supported"))
	default:
		other, ok := structs[name]
		if !ok {
			dst.tableErrs = append(dst.tableErrs, g.err(fileName, line, "embedded type %s is not a struct in this package", name))
			return
		}
		other.embedded = true
		g.expand(name, other, structs, stack)
		if !other.hasTag && len(other.cols) == 0 {
			dst.tableErrs = append(dst.tableErrs, g.err(fileName, line, "embedded type %s has no columns", name))
			return
		}
		dst.hasTag = dst.hasTag || other.hasTag
		for _, col := range other.cols {
			if prev, ok := seenCol[col.colName]; ok {
				dst.tableErrs = append(dst.tableErrs, g.err(fileName, line, "column name %s duplicates row %d", col.colName, prev))
				continue
			}
			if prev, ok := seenField[col.field]; ok {
				dst.tableErrs = append(dst.tableErrs, g.err(fileName, line, "field %s duplicates row %d", col.field, prev))
				continue
			}
			seenCol[col.colName] = col.line
			seenField[col.field] = col.line
			dst.cols = append(dst.cols, col)
		}
	}
}

const (
	embedOK int = iota
	embedCross
	embedGeneric
	embedBad
)

func embedName(expr ast.Expr) (string, int) {
	if star, ok := expr.(*ast.StarExpr); ok {
		expr = star.X
	}
	switch e := expr.(type) {
	case *ast.Ident:
		return e.Name, embedOK
	case *ast.SelectorExpr:
		return "", embedCross
	case *ast.IndexExpr, *ast.IndexListExpr:
		return "", embedGeneric
	default:
		return "", embedBad
	}
}

func reservedField(name string) bool {
	switch name {
	case "Scan", "SQLName", "TableBase", "Columns", "Row", "PrimaryKey", "Indexes", "Checks", "ColumnSpecs":
		return true
	default:
		return false
	}
}

func tagValue(f *ast.Field) string {
	if f.Tag == nil {
		return ""
	}
	unquoted, err := strconv.Unquote(f.Tag.Value)
	if err != nil {
		return ""
	}
	return reflect.StructTag(unquoted).Get("mist")
}

func (g *loader) parseTag(fileName string, line int, field string, expr ast.Expr, raw string) (column, error) {
	parts, err := splitTag(raw)
	if err != nil {
		return column{}, g.err(fileName, line, "field %s %s", field, err.Error())
	}
	if len(parts) < 2 || parts[0] == "" || parts[1] == "" {
		return column{}, g.err(fileName, line, "field %s tag must be name,type", field)
	}
	goType, err := exprString(expr)
	if err != nil {
		return column{}, g.err(fileName, line, "field %s %s", field, err.Error())
	}
	col := column{
		line: line, file: fileName, field: field, path: field,
		goType: goType, colName: parts[0], sqlType: parts[1],
	}
	var hasOnDelete, hasOnUpdate, sawDefault, sawPrec bool
	for _, mod := range parts[2:] {
		if mod == "" {
			continue
		}
		key, val, ok := strings.Cut(mod, "=")
		switch key {
		case "pk":
			if ok {
				return column{}, g.err(fileName, line, "pk does not take an argument")
			}
			col.pk = true
		case "notnull":
			if ok {
				return column{}, g.err(fileName, line, "notnull does not take an argument")
			}
			col.notNull = true
		case "unique":
			col.unique = true
			if ok {
				name, err := unquoteMod(val)
				if err != nil || name == "" {
					return column{}, g.err(fileName, line, "unique name cannot be empty")
				}
				col.uniqueName = name
			}
		case "default":
			if !ok || val == "" {
				return column{}, g.err(fileName, line, "default requires a value")
			}
			if sawDefault {
				return column{}, g.err(fileName, line, "default and defaultnow cannot both be set")
			}
			lit, err := unquoteMod(val)
			if err != nil || lit == "" {
				return column{}, g.err(fileName, line, "default requires a value")
			}
			col.def = lit
			sawDefault = true
		case "defaultnow":
			if ok {
				return column{}, g.err(fileName, line, "defaultnow does not take an argument")
			}
			if sawDefault {
				return column{}, g.err(fileName, line, "default and defaultnow cannot both be set")
			}
			col.def = "now()"
			sawDefault = true
		case "references":
			table, columnName, found := strings.Cut(val, ".")
			if !found || table == "" || columnName == "" || strings.Contains(columnName, ".") {
				return column{}, g.err(fileName, line, "references must be table.column")
			}
			col.refTable, col.refCol = table, columnName
		case "ondelete", "onupdate":
			if _, known := actions[val]; !ok || !known {
				return column{}, g.err(fileName, line, "%s must be cascade, restrict, set null, set default, or no action", key)
			}
			if key == "ondelete" {
				hasOnDelete = true
				col.onDelete = val
			} else {
				hasOnUpdate = true
				col.onUpdate = val
			}
		case "length":
			n, err := strconv.Atoi(val)
			if !ok || err != nil || n <= 0 {
				return column{}, g.err(fileName, line, "length must be a positive integer")
			}
			col.length = n
		case "precision":
			n, err := strconv.Atoi(val)
			if !ok || err != nil || n <= 0 {
				return column{}, g.err(fileName, line, "precision must be a positive integer")
			}
			if sawPrec {
				return column{}, g.err(fileName, line, "precision was set twice")
			}
			col.precision = n
			sawPrec = true
		case "scale":
			n, err := strconv.Atoi(val)
			if !ok || err != nil || n < 0 {
				return column{}, g.err(fileName, line, "scale must be a non-negative integer")
			}
			col.scale = n
		case "precis":
			p, s, found := strings.Cut(val, ".")
			if !found {
				return column{}, g.err(fileName, line, "precis must be precision.scale")
			}
			pn, err1 := strconv.Atoi(p)
			sn, err2 := strconv.Atoi(s)
			if err1 != nil || err2 != nil || pn <= 0 || sn < 0 {
				return column{}, g.err(fileName, line, "precis must be precision.scale")
			}
			if sawPrec {
				return column{}, g.err(fileName, line, "precision was set twice")
			}
			col.precision, col.scale = pn, sn
			sawPrec = true
		case "generated":
			if !ok || val == "" {
				return column{}, g.err(fileName, line, "generated requires an expression")
			}
			expr, err := unquoteMod(val)
			if err != nil || expr == "" {
				return column{}, g.err(fileName, line, "generated requires an expression")
			}
			col.generated = expr
		case "identity":
			if ok {
				return column{}, g.err(fileName, line, "identity does not take an argument")
			}
			col.identity = true
		case "enum":
			name, err := unquoteMod(val)
			if !ok || err != nil || name == "" {
				return column{}, g.err(fileName, line, "enum requires a type name")
			}
			col.enumName = name
		default:
			return column{}, g.err(fileName, line, "unknown modifier %s", key)
		}
	}
	if (hasOnDelete || hasOnUpdate) && col.refTable == "" {
		return column{}, g.err(fileName, line, "ondelete / onupdate require references")
	}
	if col.length > 0 && col.sqlType != "varchar" && col.sqlType != "char" {
		return column{}, g.err(fileName, line, "length is only for varchar or char")
	}
	if (col.precision > 0 || col.scale > 0) && col.sqlType != "numeric" && col.sqlType != "decimal" {
		return column{}, g.err(fileName, line, "precision / scale are only for numeric")
	}
	if col.scale > 0 && col.precision == 0 {
		return column{}, g.err(fileName, line, "scale requires precision")
	}
	if col.identity {
		if _, ok := integerTypes[col.sqlType]; !ok {
			return column{}, g.err(fileName, line, "identity is only for integer columns")
		}
		if col.def != "" {
			return column{}, g.err(fileName, line, "identity cannot be combined with default")
		}
		if col.generated != "" {
			return column{}, g.err(fileName, line, "identity cannot be combined with generated")
		}
	}
	if col.generated != "" && col.def != "" {
		return column{}, g.err(fileName, line, "generated cannot be combined with default")
	}
	if col.def == "now()" && !timeTypes[col.sqlType] && !strings.Contains(raw, "default=") {
		return column{}, g.err(fileName, line, "defaultnow is only for time columns")
	}
	if col.sqlType == "enum" && col.enumName == "" {
		return column{}, g.err(fileName, line, "enum requires a type name")
	}
	if col.enumName != "" && col.sqlType != "enum" {
		return column{}, g.err(fileName, line, "enum is only valid on an enum type")
	}
	return col, nil
}

func splitTag(raw string) ([]string, error) {
	var parts []string
	var b strings.Builder
	inQuote := false
	for i := 0; i < len(raw); i++ {
		c := raw[i]
		if c == '\\' && inQuote && i+1 < len(raw) {
			b.WriteByte(c)
			i++
			b.WriteByte(raw[i])
			continue
		}
		if c == '"' {
			inQuote = !inQuote
			b.WriteByte(c)
			continue
		}
		if c == ',' && !inQuote {
			parts = append(parts, strings.TrimSpace(b.String()))
			b.Reset()
			continue
		}
		b.WriteByte(c)
	}
	if inQuote {
		return nil, fmt.Errorf("quotes inside the tag are not paired")
	}
	parts = append(parts, strings.TrimSpace(b.String()))
	return parts, nil
}

func unquoteMod(val string) (string, error) {
	if strings.HasPrefix(val, `"`) {
		return strconv.Unquote(val)
	}
	return val, nil
}

func (g *loader) validate() error {
	var dialect string
	for i := range g.tables {
		d := g.tables[i].dialect
		if d == "" {
			d = "pg"
			g.tables[i].dialect = d
		}
		if dialect == "" {
			dialect = d
			continue
		}
		if d != dialect {
			g.fail(g.tables[i].file, g.tables[i].line, "a directory can use only one dialect")
		}
	}
	seenVar := map[string]string{}
	seenSQL := map[string]*table{}
	var pks int
	for i := range g.tables {
		t := &g.tables[i]
		if t.sqlName == "" {
			g.fail(t.file, t.line, "type %s Table().Name cannot be empty", t.typeName)
		}
		if prev, ok := seenVar[t.varName]; ok {
			g.fail(t.file, t.line, "exported name %s collides with type %s", t.varName, prev)
		}
		seenVar[t.varName] = t.typeName
		if prev, ok := seenSQL[t.sqlName]; ok && t.sqlName != "" {
			g.fail(t.file, t.line, "table name %s duplicates type %s", t.sqlName, prev.typeName)
		}
		seenSQL[t.sqlName] = t
		pks = 0
		for _, c := range t.cols {
			if c.pk {
				pks++
			}
		}
		g.checkTableConstraints(t, pks)
		inPK := map[string]struct{}{}
		for _, name := range t.primary {
			inPK[name] = struct{}{}
		}
		for _, c := range t.cols {
			if t.dialect == "mysql" && c.identity {
				g.fail(c.file, c.line, "column %s: MySQL has no identity", c.colName)
			}
			g.checkType(c, inPK, t.dialect)
		}
	}
	for _, t := range g.tables {
		for _, c := range t.cols {
			if c.refTable == "" {
				continue
			}
			other, ok := seenSQL[c.refTable]
			if !ok {
				g.fail(c.file, c.line, "references=%s.%s: table %s was not found", c.refTable, c.refCol, c.refTable)
				continue
			}
			found := false
			for _, oc := range other.cols {
				if oc.colName == c.refCol {
					found = true
					break
				}
			}
			if !found {
				g.fail(c.file, c.line, "references=%s.%s: column %s was not found", c.refTable, c.refCol, c.refCol)
			}
		}
	}
	return g.error()
}

func (g *loader) checkTableConstraints(t *table, columnPKs int) {
	names := map[string]struct{}{}
	for _, c := range t.cols {
		names[c.colName] = struct{}{}
	}
	if len(t.primary) > 0 && columnPKs > 0 {
		g.fail(t.file, t.line, "type %s field pk conflicts with Table().PrimaryKey", t.typeName)
	}
	if len(t.primary) == 0 && columnPKs > 1 {
		g.fail(t.file, t.line, "type %s has %d pk fields. Put a composite primary key on Table().PrimaryKey", t.typeName, columnPKs)
	}
	if len(t.primary) == 0 {
		for _, c := range t.cols {
			if c.pk {
				t.primary = []string{c.colName}
			}
		}
	}
	seenPK := map[string]struct{}{}
	for _, name := range t.primary {
		if _, ok := names[name]; !ok {
			g.fail(t.file, t.line, "PrimaryKey column %s was not found", name)
		}
		if _, ok := seenPK[name]; ok {
			g.fail(t.file, t.line, "PrimaryKey column %s is duplicated", name)
		}
		seenPK[name] = struct{}{}
	}
	seenIdx := map[string]struct{}{}
	for _, idx := range t.indexes {
		if idx.name == "" {
			g.fail(t.file, t.line, "index is missing a name")
		}
		if _, ok := seenIdx[idx.name]; ok {
			g.fail(t.file, t.line, "index name %s is duplicated", idx.name)
		}
		seenIdx[idx.name] = struct{}{}
		if len(idx.columns) == 0 {
			g.fail(t.file, t.line, "index %s is missing columns", idx.name)
		}
		for _, name := range idx.columns {
			if _, ok := names[name]; !ok {
				g.fail(t.file, t.line, "index %s: column %s was not found", idx.name, name)
			}
		}
	}
	seenCheck := map[string]struct{}{}
	for _, chk := range t.checks {
		if chk.name == "" {
			g.fail(t.file, t.line, "CHECK is missing a name")
		}
		if chk.expr == "" {
			g.fail(t.file, t.line, "CHECK %s is missing an expression", chk.name)
		}
		if _, ok := seenCheck[chk.name]; ok {
			g.fail(t.file, t.line, "CHECK name %s is duplicated", chk.name)
		}
		seenCheck[chk.name] = struct{}{}
	}
}

func (g *loader) checkType(c column, inPK map[string]struct{}, dialect string) {
	if dialect == "mysql" && strings.Contains(c.sqlType, "[]") {
		g.fail(c.file, c.line, "column %s type %s: MySQL has no arrays", c.colName, c.sqlType)
		return
	}
	if dialect == "mysql" {
		if msg, banned := mysqlReject[c.sqlType]; banned {
			g.fail(c.file, c.line, "column %s type %s: %s", c.colName, c.sqlType, msg)
			return
		}
	}
	want, ok := goTypeFor(c.sqlType, dialect)
	if !ok {
		g.fail(c.file, c.line, "column %s type %s cannot be generated yet", c.colName, c.sqlType)
		return
	}
	base, ptr, err := splitPtr(c.goType)
	if err != nil {
		g.fail(c.file, c.line, "column %s %s", c.colName, err.Error())
		return
	}
	_, pkCol := inPK[c.colName]
	notNull := c.notNull || c.pk || c.identity || pkCol
	if base != want {
		g.fail(c.file, c.line, "column %s is %s; field type should be %s, got %s", c.colName, c.sqlType, want, base)
		return
	}
	if notNull && ptr {
		g.fail(c.file, c.line, "column %s is not null; field type should be %s, got %s", c.colName, want, c.goType)
		return
	}
	if !notNull && !ptr {
		g.fail(c.file, c.line, "column %s is nullable; field type should be *%s, got %s", c.colName, want, c.goType)
	}
}

var mysqlToGo = map[string]string{
	"bigint":    "int64",
	"int":       "int32",
	"integer":   "int32",
	"smallint":  "int16",
	"tinyint":   "int8",
	"varchar":   "string",
	"char":      "string",
	"text":      "string",
	"datetime":  "time.Time",
	"timestamp": "time.Time",
	"date":      "time.Time",
	"time":      "time.Time",
	"json":      "json.RawMessage",
	"decimal":   "string",
	"numeric":   "string",
	"blob":      "[]byte",
}

var mysqlReject = map[string]string{
	"boolean":     "MySQL has no boolean; use tinyint",
	"bool":        "MySQL has no boolean; use tinyint",
	"timestamptz": "MySQL has no timestamptz; use datetime",
	"jsonb":       "MySQL has no jsonb; use json",
	"uuid":        "MySQL has no uuid; use char",
	"bytea":       "MySQL has no bytea; use blob",
	"bigserial":   "MySQL has no bigserial; use bigint",
	"serial":      "MySQL has no serial; use int",
	"smallserial": "MySQL has no smallserial; use smallint",
	"interval":    "MySQL has no interval",
	"enum":        "this MySQL generator does not emit enum",
}

func goTypeFor(sqlType, dialect string) (string, bool) {
	table := sqlToGo
	if dialect == "mysql" {
		table = mysqlToGo
	}
	if t, ok := table[sqlType]; ok {
		return t, true
	}
	if dialect == "mysql" {
		return "", false
	}
	base, dims := splitArray(sqlType)
	elem, ok := sqlToGo[base]
	if !ok || dims == 0 {
		return "", false
	}
	return strings.Repeat("[]", dims) + elem, true
}

func splitArray(sqlType string) (string, int) {
	dims := strings.Count(sqlType, "[]")
	if dims == 0 {
		return sqlType, 0
	}
	return strings.ReplaceAll(sqlType, "[]", ""), dims
}

func splitPtr(goType string) (string, bool, error) {
	if strings.HasPrefix(goType, "**") {
		return "", false, fmt.Errorf("multiple pointer levels are not supported: %s", goType)
	}
	if strings.HasPrefix(goType, "*") {
		return goType[1:], true, nil
	}
	return goType, false, nil
}

func exprString(expr ast.Expr) (string, error) {
	switch e := expr.(type) {
	case *ast.Ident:
		return e.Name, nil
	case *ast.SelectorExpr:
		pkg, ok := e.X.(*ast.Ident)
		if !ok {
			return "", fmt.Errorf("unsupported field type")
		}
		return pkg.Name + "." + e.Sel.Name, nil
	case *ast.StarExpr:
		inner, err := exprString(e.X)
		if err != nil {
			return "", err
		}
		return "*" + inner, nil
	case *ast.ArrayType:
		if e.Len != nil {
			return "", fmt.Errorf("arrays are not supported, only []byte")
		}
		inner, err := exprString(e.Elt)
		if err != nil {
			return "", err
		}
		return "[]" + inner, nil
	default:
		return "", fmt.Errorf("unsupported field type")
	}
}

func compositeName(expr ast.Expr) (string, string, bool) {
	sel, ok := expr.(*ast.SelectorExpr)
	if !ok {
		return "", "", false
	}
	pkg, ok := sel.X.(*ast.Ident)
	if !ok {
		return "", "", false
	}
	return pkg.Name, sel.Sel.Name, true
}

func recvType(fn *ast.FuncDecl) (string, bool) {
	if len(fn.Recv.List) != 1 {
		return "", false
	}
	t := fn.Recv.List[0].Type
	if star, ok := t.(*ast.StarExpr); ok {
		t = star.X
	}
	id, ok := t.(*ast.Ident)
	if !ok {
		return "", false
	}
	return id.Name, true
}

type tableBody struct {
	sqlName string
	dialect string
	primary []string
	indexes []indexSpec
	checks  []checkSpec
}

func tableLiteral(fn *ast.FuncDecl) (tableBody, error) {
	if fn.Body == nil || len(fn.Body.List) != 1 {
		return tableBody{}, fmt.Errorf("must be a single return of pg.TableMeta{...} or mysql.TableMeta{...}")
	}
	ret, ok := fn.Body.List[0].(*ast.ReturnStmt)
	if !ok || len(ret.Results) != 1 {
		return tableBody{}, fmt.Errorf("must be a single return of pg.TableMeta{...} or mysql.TableMeta{...}")
	}
	lit, ok := ret.Results[0].(*ast.CompositeLit)
	if !ok {
		return tableBody{}, fmt.Errorf("must return pg.TableMeta{...} or mysql.TableMeta{...}")
	}
	pkgName, typeName, named := compositeName(lit.Type)
	if !named || typeName != "TableMeta" || (pkgName != "pg" && pkgName != "mysql") {
		return tableBody{}, fmt.Errorf("must return pg.TableMeta{...} or mysql.TableMeta{...}")
	}
	var body tableBody
	body.dialect = pkgName
	saw := false
	for _, elt := range lit.Elts {
		kv, ok := elt.(*ast.KeyValueExpr)
		if !ok {
			return tableBody{}, fmt.Errorf("must be a keyed composite literal")
		}
		id, ok := kv.Key.(*ast.Ident)
		if !ok {
			return tableBody{}, fmt.Errorf("key must be an identifier")
		}
		switch id.Name {
		case "Name":
			s, err := stringLit(kv.Value)
			if err != nil || s == "" {
				return tableBody{}, fmt.Errorf("Name must be a non-empty string")
			}
			body.sqlName = s
			saw = true
		case "PrimaryKey":
			cols, err := stringList(kv.Value)
			if err != nil {
				return tableBody{}, fmt.Errorf("PrimaryKey %s", err.Error())
			}
			if len(cols) == 0 {
				return tableBody{}, fmt.Errorf("PrimaryKey cannot be an empty list")
			}
			body.primary = cols
		case "Indexes":
			indexes, err := indexList(kv.Value)
			if err != nil {
				return tableBody{}, fmt.Errorf("Indexes %s", err.Error())
			}
			body.indexes = indexes
		case "Checks":
			checks, err := checkList(kv.Value)
			if err != nil {
				return tableBody{}, fmt.Errorf("Checks %s", err.Error())
			}
			body.checks = checks
		default:
			return tableBody{}, fmt.Errorf("cannot set %s", id.Name)
		}
	}
	if !saw {
		return tableBody{}, fmt.Errorf("Name is required")
	}
	return body, nil
}

func stringLit(expr ast.Expr) (string, error) {
	bl, ok := expr.(*ast.BasicLit)
	if !ok || bl.Kind != token.STRING {
		return "", fmt.Errorf("must be a string literal")
	}
	s, err := strconv.Unquote(bl.Value)
	if err != nil {
		return "", err
	}
	return s, nil
}

func stringList(expr ast.Expr) ([]string, error) {
	if id, ok := expr.(*ast.Ident); ok && id.Name == "nil" {
		return nil, nil
	}
	lit, ok := expr.(*ast.CompositeLit)
	if !ok {
		return nil, fmt.Errorf("must be a list of strings")
	}
	out := make([]string, 0, len(lit.Elts))
	for _, elt := range lit.Elts {
		s, err := stringLit(elt)
		if err != nil || s == "" {
			return nil, fmt.Errorf("list entries must be non-empty strings")
		}
		out = append(out, s)
	}
	return out, nil
}

func indexList(expr ast.Expr) ([]indexSpec, error) {
	if id, ok := expr.(*ast.Ident); ok && id.Name == "nil" {
		return nil, nil
	}
	lit, ok := expr.(*ast.CompositeLit)
	if !ok {
		return nil, fmt.Errorf("must be an index list")
	}
	out := make([]indexSpec, 0, len(lit.Elts))
	for _, elt := range lit.Elts {
		item, ok := elt.(*ast.CompositeLit)
		if !ok {
			return nil, fmt.Errorf("each entry must be a composite literal")
		}
		var idx indexSpec
		for _, field := range item.Elts {
			kv, ok := field.(*ast.KeyValueExpr)
			if !ok {
				return nil, fmt.Errorf("must be a keyed composite literal")
			}
			id, ok := kv.Key.(*ast.Ident)
			if !ok {
				return nil, fmt.Errorf("key must be an identifier")
			}
			switch id.Name {
			case "Name":
				s, err := stringLit(kv.Value)
				if err != nil {
					return nil, err
				}
				idx.name = s
			case "Columns":
				cols, err := stringList(kv.Value)
				if err != nil {
					return nil, err
				}
				idx.columns = cols
			case "Unique":
				uniq, err := boolLit(kv.Value)
				if err != nil {
					return nil, err
				}
				idx.unique = uniq
			default:
				return nil, fmt.Errorf("unknown field %s", id.Name)
			}
		}
		out = append(out, idx)
	}
	return out, nil
}

func checkList(expr ast.Expr) ([]checkSpec, error) {
	if id, ok := expr.(*ast.Ident); ok && id.Name == "nil" {
		return nil, nil
	}
	lit, ok := expr.(*ast.CompositeLit)
	if !ok {
		return nil, fmt.Errorf("must be a CHECK list")
	}
	out := make([]checkSpec, 0, len(lit.Elts))
	for _, elt := range lit.Elts {
		item, ok := elt.(*ast.CompositeLit)
		if !ok {
			return nil, fmt.Errorf("each entry must be a composite literal")
		}
		var chk checkSpec
		for _, field := range item.Elts {
			kv, ok := field.(*ast.KeyValueExpr)
			if !ok {
				return nil, fmt.Errorf("must be a keyed composite literal")
			}
			id, ok := kv.Key.(*ast.Ident)
			if !ok {
				return nil, fmt.Errorf("key must be an identifier")
			}
			switch id.Name {
			case "Name":
				s, err := stringLit(kv.Value)
				if err != nil {
					return nil, err
				}
				chk.name = s
			case "Expr":
				s, err := stringLit(kv.Value)
				if err != nil {
					return nil, err
				}
				chk.expr = s
			default:
				return nil, fmt.Errorf("unknown field %s", id.Name)
			}
		}
		out = append(out, chk)
	}
	return out, nil
}

func boolLit(expr ast.Expr) (bool, error) {
	id, ok := expr.(*ast.Ident)
	if !ok {
		return false, fmt.Errorf("Unique must be true or false")
	}
	switch id.Name {
	case "true":
		return true, nil
	case "false":
		return false, nil
	default:
		return false, fmt.Errorf("Unique must be true or false")
	}
}

func exportName(typeName string) string {
	if strings.HasSuffix(typeName, "s") {
		return typeName
	}
	return typeName + "s"
}

func (g *loader) at(pos token.Pos) (string, int) {
	if !pos.IsValid() {
		return "", 0
	}
	p := g.fset.Position(pos)
	rel, err := filepath.Rel(g.dir, p.Filename)
	if err != nil {
		rel = p.Filename
	}
	return filepath.ToSlash(rel), p.Line
}

func (g *loader) fail(file string, line int, format string, args ...any) {
	g.errs = append(g.errs, g.err(file, line, format, args...))
}

func (g *loader) err(file string, line int, format string, args ...any) error {
	msg := fmt.Sprintf(format, args...)
	if file == "" {
		return fmt.Errorf("%s", msg)
	}
	if line == 0 {
		return fmt.Errorf("%s: %s", file, msg)
	}
	return fmt.Errorf("%s:%d: %s", file, line, msg)
}

func (g *loader) error() error {
	if len(g.errs) == 0 {
		return nil
	}
	msgs := make([]string, len(g.errs))
	for i, e := range g.errs {
		msgs[i] = e.Error()
	}
	return fmt.Errorf("%s", strings.Join(msgs, "\n"))
}

func sortTables(tables []table) {
	for i := 1; i < len(tables); i++ {
		j := i
		for j > 0 && lessTable(tables[j], tables[j-1]) {
			tables[j], tables[j-1] = tables[j-1], tables[j]
			j--
		}
	}
}

func lessTable(a, b table) bool {
	if a.file != b.file {
		return a.file < b.file
	}
	return a.pos < b.pos
}
