// Package codegen 读取结构体上的 drizzle tag，生成列描述符和扫描函数。
// 生成阶段可以看语法树。运行查询时不读 tag，也不反射字段。
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

// Generate 在 dir 写入 schema_gen.go。失败时不改已有文件。
func Generate(dir string) error {
	src, err := GenerateSource(dir)
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, outFile), src, 0o644)
}

// GenerateSource 返回将要写入的源码。
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

// shortcut: tag 按逗号切分，默认值里不能含逗号；含逗号时要换一种写法。
// shortcut: references 只接受 table.column，不接受 schema.table.column。
// shortcut: 导出名只在类型名后加 s（User → Users）。不规则复数以后由 Table() 指定。

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
}

var actions = map[string]struct{}{
	"cascade":     {},
	"restrict":    {},
	"set null":    {},
	"set default": {},
	"no action":   {},
}

type table struct {
	pos      token.Pos
	file     string
	line     int
	typeName string
	varName  string
	sqlName  string
	cols     []column
}

type column struct {
	line     int
	file     string
	field    string
	goType   string
	colName  string
	sqlType  string
	notNull  bool
	pk       bool
	refTable string
	refCol   string
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
			return nil, fmt.Errorf("%s: 包名是 %s，目录里其它文件是 %s", name, file.Name.Name, g.pkg)
		}
		collect(file, structs, methods, g)
	}
	if g.pkg == "" {
		return nil, fmt.Errorf("%s: 没有 Go 文件", dir)
	}
	for name, st := range structs {
		m, hasMethod := methods[name]
		if !hasMethod && !st.hasTag {
			continue
		}
		g.errs = append(g.errs, st.tableErrs...)
		if !hasMethod {
			g.fail(st.file, st.line, "类型 %s 有 drizzle 标签，但没有 Table() 方法", name)
			continue
		}
		for _, key := range m.extra {
			g.fail(m.file, m.line, "Table() 目前只读取 Name，不能写 %s", key)
		}
		if len(st.cols) == 0 {
			g.fail(m.file, m.line, "类型 %s 没有列", name)
			continue
		}
		g.tables = append(g.tables, table{
			pos:      st.pos,
			file:     st.file,
			line:     st.line,
			typeName: name,
			varName:  exportName(name),
			sqlName:  m.sqlName,
			cols:     st.cols,
		})
	}
	for name, m := range methods {
		if _, ok := structs[name]; !ok {
			g.fail(m.file, m.line, "Table() 的接收者 %s 不是结构体", name)
		}
	}
	sortTables(g.tables)
	if len(g.tables) == 0 && len(g.errs) == 0 {
		g.fail("", 0, "没有找到带 Table() 的结构体")
	}
	return g, nil
}

type astStruct struct {
	pos       token.Pos
	file      string
	line      int
	cols      []column
	hasTag    bool
	tableErrs []error
}

type method struct {
	file    string
	line    int
	sqlName string
	extra   []string
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
					g.fail(fileName, line, "类型 %s 重复定义（已在 %s:%d）", ts.Name.Name, prev.file, prev.line)
					continue
				}
				parsed := &astStruct{pos: ts.Pos(), file: fileName, line: line}
				g.fields(st, parsed)
				structs[ts.Name.Name] = parsed
			}
		case *ast.FuncDecl:
			if d.Name.Name != "Table" || d.Recv == nil {
				continue
			}
			recv, ok := recvType(d)
			if !ok {
				fileName, line := g.at(d.Pos())
				g.fail(fileName, line, "Table() 的接收者必须是具名结构体")
				continue
			}
			fileName, line := g.at(d.Pos())
			sqlName, extra, err := tableLiteral(d)
			if err != nil {
				g.fail(fileName, line, "%s.Table() %s", recv, err.Error())
				sqlName = ""
			}
			if prev, ok := methods[recv]; ok {
				g.fail(fileName, line, "%s.Table() 重复定义（已在 %s:%d）", recv, prev.file, prev.line)
				continue
			}
			methods[recv] = method{file: fileName, line: line, sqlName: sqlName, extra: extra}
		}
	}
}

func (g *loader) fields(st *ast.StructType, dst *astStruct) {
	if st.Fields == nil {
		return
	}
	seen := map[string]int{}
	for _, f := range st.Fields.List {
		fileName, line := g.at(f.Pos())
		if len(f.Names) == 0 {
			dst.tableErrs = append(dst.tableErrs, g.err(fileName, line, "不支持嵌入字段"))
			continue
		}
		if len(f.Names) != 1 {
			dst.tableErrs = append(dst.tableErrs, g.err(fileName, line, "一列只能有一个字段名"))
			continue
		}
		field := f.Names[0].Name
		raw := tagValue(f)
		if raw == "" {
			if ast.IsExported(field) {
				dst.tableErrs = append(dst.tableErrs, g.err(fileName, line, "导出字段 %s 缺少 drizzle 标签", field))
			}
			continue
		}
		dst.hasTag = true
		if !ast.IsExported(field) {
			dst.tableErrs = append(dst.tableErrs, g.err(fileName, line, "字段 %s 未导出，不能作为列", field))
			continue
		}
		if field == "Scan" || field == "SQLName" || field == "TableBase" {
			dst.tableErrs = append(dst.tableErrs, g.err(fileName, line, "字段名 %s 与生成的表描述符冲突", field))
			continue
		}
		col, err := g.parseTag(fileName, line, field, f.Type, raw)
		if err != nil {
			dst.tableErrs = append(dst.tableErrs, err)
			continue
		}
		if prev, ok := seen[col.colName]; ok {
			dst.tableErrs = append(dst.tableErrs, g.err(fileName, line, "列名 %s 与第 %d 行重复", col.colName, prev))
			continue
		}
		seen[col.colName] = line
		dst.cols = append(dst.cols, col)
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
	return reflect.StructTag(unquoted).Get("drizzle")
}

func (g *loader) parseTag(fileName string, line int, field string, expr ast.Expr, raw string) (column, error) {
	parts := strings.Split(raw, ",")
	for i := range parts {
		parts[i] = strings.TrimSpace(parts[i])
	}
	if len(parts) < 2 || parts[0] == "" || parts[1] == "" {
		return column{}, g.err(fileName, line, "字段 %s 的标签必须是 name,type", field)
	}
	goType, err := exprString(expr)
	if err != nil {
		return column{}, g.err(fileName, line, "字段 %s %s", field, err.Error())
	}
	col := column{
		line: line, file: fileName, field: field,
		goType: goType, colName: parts[0], sqlType: parts[1],
	}
	var hasOnDelete, hasOnUpdate bool
	for _, mod := range parts[2:] {
		if mod == "" {
			continue
		}
		key, val, ok := strings.Cut(mod, "=")
		switch key {
		case "pk":
			if ok {
				return column{}, g.err(fileName, line, "pk 不接受参数")
			}
			col.pk = true
		case "notnull":
			if ok {
				return column{}, g.err(fileName, line, "notnull 不接受参数")
			}
			col.notNull = true
		case "unique":
			if ok && val == "" {
				return column{}, g.err(fileName, line, "unique 的名字不能为空")
			}
		case "default":
			if !ok || val == "" {
				return column{}, g.err(fileName, line, "default 需要一个值")
			}
		case "references":
			table, columnName, ok := strings.Cut(val, ".")
			if !ok || table == "" || columnName == "" || strings.Contains(columnName, ".") {
				return column{}, g.err(fileName, line, "references 必须是 table.column")
			}
			col.refTable, col.refCol = table, columnName
		case "ondelete", "onupdate":
			if _, known := actions[val]; !ok || !known {
				return column{}, g.err(fileName, line, "%s 必须是 cascade、restrict、set null、set default 或 no action", key)
			}
			if key == "ondelete" {
				hasOnDelete = true
			} else {
				hasOnUpdate = true
			}
		default:
			return column{}, g.err(fileName, line, "未知修饰符 %s", key)
		}
	}
	if (hasOnDelete || hasOnUpdate) && col.refTable == "" {
		return column{}, g.err(fileName, line, "ondelete / onupdate 要和 references 一起用")
	}
	return col, nil
}

func (g *loader) validate() error {
	seenVar := map[string]string{}
	seenSQL := map[string]*table{}
	var pks int
	for i := range g.tables {
		t := &g.tables[i]
		if t.sqlName == "" {
			g.fail(t.file, t.line, "类型 %s 的 Table().Name 不能为空", t.typeName)
		}
		if prev, ok := seenVar[t.varName]; ok {
			g.fail(t.file, t.line, "导出名 %s 与类型 %s 冲突", t.varName, prev)
		}
		seenVar[t.varName] = t.typeName
		if prev, ok := seenSQL[t.sqlName]; ok && t.sqlName != "" {
			g.fail(t.file, t.line, "表名 %s 与类型 %s 重复", t.sqlName, prev.typeName)
		}
		seenSQL[t.sqlName] = t
		pks = 0
		for _, c := range t.cols {
			if c.pk {
				pks++
			}
			g.checkType(c)
		}
		if pks > 1 {
			g.fail(t.file, t.line, "类型 %s 有 %d 个 pk。复合主键写在 Table()，这一截还没解析", t.typeName, pks)
		}
	}
	for _, t := range g.tables {
		for _, c := range t.cols {
			if c.refTable == "" {
				continue
			}
			other, ok := seenSQL[c.refTable]
			if !ok {
				g.fail(c.file, c.line, "references=%s.%s 找不到表 %s", c.refTable, c.refCol, c.refTable)
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
				g.fail(c.file, c.line, "references=%s.%s 找不到列 %s", c.refTable, c.refCol, c.refCol)
			}
		}
	}
	return g.error()
}

func (g *loader) checkType(c column) {
	want, ok := sqlToGo[c.sqlType]
	if !ok {
		g.fail(c.file, c.line, "列 %s 的类型 %s 还不能生成", c.colName, c.sqlType)
		return
	}
	base, ptr, err := splitPtr(c.goType)
	if err != nil {
		g.fail(c.file, c.line, "列 %s %s", c.colName, err.Error())
		return
	}
	notNull := c.notNull || c.pk
	if base != want {
		g.fail(c.file, c.line, "列 %s 是 %s，字段类型应是 %s，实际是 %s", c.colName, c.sqlType, want, base)
		return
	}
	if notNull && ptr {
		g.fail(c.file, c.line, "列 %s 非空，字段类型应是 %s，实际是 %s", c.colName, want, c.goType)
		return
	}
	if !notNull && !ptr {
		g.fail(c.file, c.line, "列 %s 可空，字段类型应是 *%s，实际是 %s", c.colName, want, c.goType)
	}
}

func splitPtr(goType string) (string, bool, error) {
	if strings.HasPrefix(goType, "**") {
		return "", false, fmt.Errorf("不支持多级指针 %s", goType)
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
			return "", fmt.Errorf("不支持的字段类型")
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
			return "", fmt.Errorf("不支持数组，只用 []byte")
		}
		inner, err := exprString(e.Elt)
		if err != nil {
			return "", err
		}
		return "[]" + inner, nil
	default:
		return "", fmt.Errorf("不支持的字段类型")
	}
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

func tableLiteral(fn *ast.FuncDecl) (string, []string, error) {
	if fn.Body == nil || len(fn.Body.List) != 1 {
		return "", nil, fmt.Errorf("必须只有一条 return pg.TableMeta{Name: \"...\"}")
	}
	ret, ok := fn.Body.List[0].(*ast.ReturnStmt)
	if !ok || len(ret.Results) != 1 {
		return "", nil, fmt.Errorf("必须只有一条 return pg.TableMeta{Name: \"...\"}")
	}
	lit, ok := ret.Results[0].(*ast.CompositeLit)
	if !ok {
		return "", nil, fmt.Errorf("必须返回 pg.TableMeta{Name: \"...\"}")
	}
	var name string
	var extra []string
	saw := false
	for _, elt := range lit.Elts {
		kv, ok := elt.(*ast.KeyValueExpr)
		if !ok {
			return "", nil, fmt.Errorf("必须使用带键的复合字面量")
		}
		id, ok := kv.Key.(*ast.Ident)
		if !ok {
			return "", nil, fmt.Errorf("键必须是标识符")
		}
		if id.Name != "Name" {
			extra = append(extra, id.Name)
			continue
		}
		bl, ok := kv.Value.(*ast.BasicLit)
		if !ok || bl.Kind != token.STRING {
			return "", nil, fmt.Errorf("Name 必须是字符串字面量")
		}
		s, err := strconv.Unquote(bl.Value)
		if err != nil || s == "" {
			return "", nil, fmt.Errorf("Name 不能为空")
		}
		name = s
		saw = true
	}
	if !saw {
		return "", nil, fmt.Errorf("必须设置 Name")
	}
	return name, extra, nil
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
