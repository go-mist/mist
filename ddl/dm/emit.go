// Package dm renders diff changes as Dameng statements.
// Enums, arrays, and PostgreSQL-only types are rejected. RETURNING is not involved.
package dm

import (
	"fmt"
	"strings"

	"github.com/go-mist/mist/ddl"
	"github.com/go-mist/mist/dialect/dm"
	"github.com/go-mist/mist/diff"
	"github.com/go-mist/mist/schema"
)

const breakpoint = "--> statement-breakpoint"

// Render writes the changes as the body of one migration.sql. No changes returns an empty string.
func Render(changes []diff.Change) (string, error) {
	stmts, err := statements(changes)
	if err != nil {
		return "", err
	}
	if len(stmts) == 0 {
		return "", nil
	}
	return strings.Join(stmts, "\n"+breakpoint+"\n") + "\n", nil
}

func statements(changes []diff.Change) ([]string, error) {
	var creates, drops []schema.Table
	var rest []diff.Change
	for _, change := range changes {
		switch change.Kind {
		case diff.CreateTable:
			creates = append(creates, *change.TableDef)
		case diff.DropTable:
			drops = append(drops, *change.TableDef)
		default:
			rest = append(rest, change)
		}
	}
	for _, change := range rest {
		if change.Kind == diff.CreateEnum || change.Kind == diff.DropEnum || change.Kind == diff.AlterEnum {
			name := ""
			if change.Enum != nil {
				name = change.Enum.Name
			}
			return nil, fmt.Errorf("dm: enum %s is not supported%s", name, at(change.Loc))
		}
	}
	var stmts []string
	created, err := createTables(creates)
	if err != nil {
		return nil, err
	}
	stmts = append(stmts, created...)
	for _, change := range rest {
		sql, err := renderChange(change)
		if err != nil {
			return nil, err
		}
		stmts = append(stmts, sql...)
	}
	dropped, err := dropTables(drops)
	if err != nil {
		return nil, err
	}
	return append(stmts, dropped...), nil
}

func renderChange(change diff.Change) ([]string, error) {
	switch change.Kind {
	case diff.AddColumn:
		line, err := columnSQL(change.Table, *change.Column, false)
		if err != nil {
			return nil, err
		}
		return []string{"ALTER TABLE " + quote(change.Table) + " ADD " + line}, nil
	case diff.DropColumn:
		return []string{"ALTER TABLE " + quote(change.Table) + " DROP COLUMN " + quote(change.Name)}, nil
	case diff.AlterColumn:
		return alterColumn(change)
	case diff.AlterPrimaryKey:
		return alterPrimaryKey(change), nil
	case diff.AddUnique:
		return []string{fmt.Sprintf("ALTER TABLE %s ADD CONSTRAINT %s UNIQUE (%s)", quote(change.Table), quote(change.Unique.Name), quoteList(change.Unique.Columns))}, nil
	case diff.DropUnique, diff.DropCheck, diff.DropForeignKey:
		return []string{"ALTER TABLE " + quote(change.Table) + " DROP CONSTRAINT " + quote(change.Name)}, nil
	case diff.AddIndex:
		return []string{indexSQL(change.Table, *change.Index)}, nil
	case diff.DropIndex:
		return []string{"DROP INDEX " + quote(change.Name)}, nil
	case diff.AddCheck:
		return []string{fmt.Sprintf("ALTER TABLE %s ADD CONSTRAINT %s CHECK (%s)", quote(change.Table), quote(change.Check.Name), change.Check.Expr)}, nil
	case diff.AddForeignKey:
		return []string{"ALTER TABLE " + quote(change.Table) + " ADD " + foreignKeySQL(*change.ForeignKey)}, nil
	default:
		return nil, fmt.Errorf("dm: unknown change %s", change.Kind)
	}
}

func alterColumn(change diff.Change) ([]string, error) {
	from, to := change.ColumnFrom, change.Column
	if from.Identity != to.Identity || from.Generated != to.Generated {
		return nil, fmt.Errorf("dm: column %s.%s identity or generated expression cannot be altered in place%s", change.Table, to.Name, at(change.Loc))
	}
	typ, err := renderType(to.Type)
	if err != nil {
		return nil, err
	}
	prefix := "ALTER TABLE " + quote(change.Table) + " MODIFY " + quote(to.Name)
	var stmts []string
	if from.Type != to.Type {
		stmts = append(stmts, prefix+" "+typ)
	}
	if from.Default != to.Default {
		if to.Default == "" {
			stmts = append(stmts, prefix+" DEFAULT NULL")
		} else {
			stmts = append(stmts, prefix+" DEFAULT "+to.Default)
		}
	}
	if from.NotNull != to.NotNull {
		if to.NotNull {
			stmts = append(stmts, prefix+" NOT NULL")
		} else {
			stmts = append(stmts, prefix+" NULL")
		}
	}
	return stmts, nil
}

func alterPrimaryKey(change diff.Change) []string {
	var stmts []string
	if len(change.PrimaryKeyFrom) > 0 {
		name := change.PrimaryKeyName
		if name == "" {
			name = change.Table + "_pkey"
		}
		stmts = append(stmts, "ALTER TABLE "+quote(change.Table)+" DROP CONSTRAINT "+quote(name))
	}
	if len(change.PrimaryKeyTo) > 0 {
		stmts = append(stmts, "ALTER TABLE "+quote(change.Table)+" ADD PRIMARY KEY ("+quoteList(change.PrimaryKeyTo)+")")
	}
	return stmts
}

func createTables(tables []schema.Table) ([]string, error) {
	if len(tables) == 0 {
		return nil, nil
	}
	ordered, ok := ddl.OrderTables(tables)
	inlineFK := ok
	if !ok {
		ordered = tables
	}
	var stmts []string
	for _, table := range ordered {
		sql, err := createTableSQL(table, inlineFK)
		if err != nil {
			return nil, err
		}
		stmts = append(stmts, sql)
		for _, idx := range table.Indexes {
			stmts = append(stmts, indexSQL(table.Name, idx))
		}
	}
	if inlineFK {
		return stmts, nil
	}
	for _, table := range ordered {
		for _, fk := range table.ForeignKeys {
			stmts = append(stmts, "ALTER TABLE "+quote(table.Name)+" ADD "+foreignKeySQL(fk))
		}
	}
	return stmts, nil
}

func dropTables(tables []schema.Table) ([]string, error) {
	if len(tables) == 0 {
		return nil, nil
	}
	ordered, ok := ddl.OrderTables(tables)
	if !ok {
		ordered = tables
	}
	var stmts []string
	for i := len(ordered) - 1; i >= 0; i-- {
		stmts = append(stmts, "DROP TABLE "+quote(ordered[i].Name))
	}
	return stmts, nil
}

func createTableSQL(table schema.Table, withFK bool) (string, error) {
	pkInline := len(table.PrimaryKey) == 1
	pkCol := ""
	if pkInline {
		pkCol = table.PrimaryKey[0]
	}
	var lines []string
	for _, col := range table.Columns {
		line, err := columnSQL(table.Name, col, pkInline && col.Name == pkCol)
		if err != nil {
			return "", err
		}
		lines = append(lines, "\t"+line)
	}
	if !pkInline && len(table.PrimaryKey) > 0 {
		lines = append(lines, "\tPRIMARY KEY ("+quoteList(table.PrimaryKey)+")")
	}
	for _, unique := range table.Uniques {
		lines = append(lines, fmt.Sprintf("\tCONSTRAINT %s UNIQUE (%s)", quote(unique.Name), quoteList(unique.Columns)))
	}
	for _, check := range table.Checks {
		lines = append(lines, fmt.Sprintf("\tCONSTRAINT %s CHECK (%s)", quote(check.Name), check.Expr))
	}
	if withFK {
		for _, fk := range table.ForeignKeys {
			lines = append(lines, "\t"+foreignKeySQL(fk))
		}
	}
	return "CREATE TABLE " + quote(table.Name) + " (\n" + strings.Join(lines, ",\n") + "\n)", nil
}

func columnSQL(table string, col schema.Column, primary bool) (string, error) {
	typ, err := renderType(col.Type)
	if err != nil {
		return "", fmt.Errorf("dm: table %s column %s: %w", table, col.Name, err)
	}
	sql := quote(col.Name) + " " + typ
	if col.Generated != "" {
		sql += " GENERATED ALWAYS AS (" + col.Generated + ")"
	}
	if col.Identity {
		sql += " IDENTITY(1,1)"
	}
	if col.Default != "" && col.Generated == "" && !col.Identity {
		sql += " DEFAULT " + col.Default
	}
	if col.NotNull || primary {
		sql += " NOT NULL"
	}
	if primary {
		sql += " PRIMARY KEY"
	}
	return sql, nil
}

func foreignKeySQL(fk schema.ForeignKey) string {
	sql := fmt.Sprintf("CONSTRAINT %s FOREIGN KEY (%s) REFERENCES %s (%s)",
		quote(fk.Name), quoteList(fk.Columns), quote(fk.RefTable), quoteList(fk.RefColumns))
	if fk.OnDelete != "" {
		sql += " ON DELETE " + strings.ToUpper(fk.OnDelete)
	}
	return sql
}

func indexSQL(table string, idx schema.Index) string {
	unique := ""
	if idx.Unique {
		unique = "UNIQUE "
	}
	return "CREATE " + unique + "INDEX " + quote(idx.Name) + " ON " + quote(table) + " (" + quoteList(idx.Columns) + ")"
}

func renderType(typ schema.Type) (string, error) {
	if typ.Enum || typ.ArrayDims > 0 {
		return "", fmt.Errorf("type %s is not supported", typ.Name)
	}
	switch typ.Name {
	case "bigserial", "bigint", "int8":
		return "BIGINT", nil
	case "serial", "integer", "int4", "int":
		return "INT", nil
	case "smallserial", "smallint", "int2":
		return "SMALLINT", nil
	case "boolean", "bool":
		return "BIT", nil
	case "text":
		return "CLOB", nil
	case "bytea":
		return "BLOB", nil
	case "timestamptz":
		return "TIMESTAMP WITH TIME ZONE", nil
	case "timestamp", "datetime":
		return "TIMESTAMP", nil
	case "json":
		return "JSON", nil
	case "jsonb", "uuid":
		return "", fmt.Errorf("type %s is not supported", typ.Name)
	case "varchar", "varchar2":
		if typ.Length <= 0 || typ.Length > 32767 {
			return "", fmt.Errorf("varchar length must be from 1 to 32767")
		}
		return fmt.Sprintf("VARCHAR(%d)", typ.Length), nil
	case "numeric", "decimal", "number":
		if typ.Precision > 0 && typ.Scale > 0 {
			return fmt.Sprintf("NUMBER(%d,%d)", typ.Precision, typ.Scale), nil
		}
		if typ.Precision > 0 {
			return fmt.Sprintf("NUMBER(%d)", typ.Precision), nil
		}
		return "NUMBER", nil
	default:
		return "", fmt.Errorf("type %s is not supported", typ.Name)
	}
}

func quote(name string) string { return dm.Dialect{}.EscapeName(name) }

func quoteList(names []string) string {
	parts := make([]string, len(names))
	for i, name := range names {
		parts[i] = quote(name)
	}
	return strings.Join(parts, ", ")
}

func at(loc schema.Loc) string {
	if loc.File == "" {
		return ""
	}
	return fmt.Sprintf(" (%s:%d)", loc.File, loc.Line)
}
