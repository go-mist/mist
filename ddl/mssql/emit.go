// Package mssql renders diff changes as SQL Server statements.
// Enums, arrays, and jsonb are rejected. RETURNING is not involved.
// A json column is nvarchar(max) plus a check that isjson(column) = 1, so a read-back can fold it to json.
package mssql

import (
	"fmt"
	"strings"

	"github.com/go-mist/mist/ddl"
	"github.com/go-mist/mist/dialect/mssql"
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
			return nil, fmt.Errorf("mssql: enum %s is not supported%s", name, at(change.Loc))
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
		line, err := columnSQL(*change.Column, false)
		if err != nil {
			return nil, err
		}
		stmts := []string{"ALTER TABLE " + quote(change.Table) + " ADD " + line}
		if jsonCheck := jsonConstraintSQL(change.Table, *change.Column); jsonCheck != "" {
			stmts = append(stmts, "ALTER TABLE "+quote(change.Table)+" ADD "+jsonCheck)
		}
		return stmts, nil
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
		return []string{"DROP INDEX " + quote(change.Name) + " ON " + quote(change.Table)}, nil
	case diff.AddCheck:
		return []string{fmt.Sprintf("ALTER TABLE %s ADD CONSTRAINT %s CHECK (%s)", quote(change.Table), quote(change.Check.Name), change.Check.Expr)}, nil
	case diff.AddForeignKey:
		return []string{"ALTER TABLE " + quote(change.Table) + " ADD " + foreignKeySQL(*change.ForeignKey)}, nil
	default:
		return nil, fmt.Errorf("mssql: unknown change %s", change.Kind)
	}
}

func alterColumn(change diff.Change) ([]string, error) {
	from, to := change.ColumnFrom, change.Column
	if from.Identity != to.Identity || from.Generated != to.Generated || from.Default != to.Default {
		return nil, fmt.Errorf("mssql: column %s.%s identity, generated expression, or default cannot be altered in place%s", change.Table, to.Name, at(change.Loc))
	}
	typ, err := renderType(to.Type)
	if err != nil {
		return nil, fmt.Errorf("mssql: column %s.%s: %w", change.Table, to.Name, err)
	}
	null := " NULL"
	if to.NotNull {
		null = " NOT NULL"
	}
	return []string{"ALTER TABLE " + quote(change.Table) + " ALTER COLUMN " + quote(to.Name) + " " + typ + null}, nil
}

func alterPrimaryKey(change diff.Change) []string {
	var stmts []string
	name := change.PrimaryKeyName
	if name == "" {
		name = "pk_" + change.Table
	}
	if len(change.PrimaryKeyFrom) > 0 {
		stmts = append(stmts, "ALTER TABLE "+quote(change.Table)+" DROP CONSTRAINT "+quote(name))
	}
	if len(change.PrimaryKeyTo) > 0 {
		stmts = append(stmts, "ALTER TABLE "+quote(change.Table)+" ADD CONSTRAINT "+quote(name)+" PRIMARY KEY ("+quoteList(change.PrimaryKeyTo)+")")
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
		line, err := columnSQL(col, pkInline && col.Name == pkCol)
		if err != nil {
			return "", err
		}
		lines = append(lines, "\t"+line)
		if check := jsonConstraintSQL(table.Name, col); check != "" {
			lines = append(lines, "\t"+check)
		}
	}
	if !pkInline && len(table.PrimaryKey) > 0 {
		name := table.PrimaryKeyName
		if name == "" {
			name = "pk_" + table.Name
		}
		lines = append(lines, "\tCONSTRAINT "+quote(name)+" PRIMARY KEY ("+quoteList(table.PrimaryKey)+")")
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

func columnSQL(col schema.Column, primary bool) (string, error) {
	typ, identity, err := columnType(col)
	if err != nil {
		return "", fmt.Errorf("mssql: column %s: %w", col.Name, err)
	}
	sql := quote(col.Name) + " " + typ
	if col.Generated != "" {
		sql += " AS (" + col.Generated + ") PERSISTED"
	}
	if identity {
		sql += " IDENTITY(1,1)"
	}
	if col.NotNull || primary || identity {
		sql += " NOT NULL"
	} else {
		sql += " NULL"
	}
	if col.Default != "" && col.Generated == "" && !identity {
		sql += " DEFAULT " + col.Default
	}
	if primary {
		sql += " PRIMARY KEY"
	}
	return sql, nil
}

func columnType(col schema.Column) (string, bool, error) {
	typ, err := renderType(col.Type)
	if err != nil {
		return "", false, err
	}
	identity := col.Identity || isSerial(col.Type.Name)
	return typ, identity, nil
}

func isSerial(name string) bool {
	switch name {
	case "serial", "bigserial", "smallserial":
		return true
	default:
		return false
	}
}

func jsonConstraintSQL(table string, col schema.Column) string {
	if col.Type.Name != "json" {
		return ""
	}
	return fmt.Sprintf("CONSTRAINT %s CHECK (isjson(%s) = 1)", quote(table+"_"+col.Name+"_json"), quote(col.Name))
}

func foreignKeySQL(fk schema.ForeignKey) string {
	sql := fmt.Sprintf("CONSTRAINT %s FOREIGN KEY (%s) REFERENCES %s (%s)",
		quote(fk.Name), quoteList(fk.Columns), quote(fk.RefTable), quoteList(fk.RefColumns))
	if fk.OnDelete != "" {
		sql += " ON DELETE " + strings.ToUpper(fk.OnDelete)
	}
	if fk.OnUpdate != "" {
		sql += " ON UPDATE " + strings.ToUpper(fk.OnUpdate)
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
	case "jsonb":
		return "", fmt.Errorf("type %s is not supported", typ.Name)
	case "boolean", "bool":
		return "bit", nil
	case "uuid":
		return "uniqueidentifier", nil
	case "timestamptz":
		return "datetimeoffset", nil
	case "timestamp", "datetime":
		return "datetime2", nil
	case "bytea", "blob":
		return "varbinary(max)", nil
	case "json", "text":
		return "nvarchar(max)", nil
	case "varchar", "nvarchar", "char", "nchar":
		name := typ.Name
		if name == "varchar" || name == "char" {
			name = "n" + name
		}
		if typ.Length > 0 {
			return fmt.Sprintf("%s(%d)", name, typ.Length), nil
		}
		return name + "(max)", nil
	case "integer", "int", "int4", "serial":
		return "int", nil
	case "bigint", "int8", "bigserial":
		return "bigint", nil
	case "smallint", "int2", "smallserial":
		return "smallint", nil
	case "numeric", "decimal":
		if typ.Precision > 0 && typ.Scale > 0 {
			return fmt.Sprintf("decimal(%d,%d)", typ.Precision, typ.Scale), nil
		}
		if typ.Precision > 0 {
			return fmt.Sprintf("decimal(%d)", typ.Precision), nil
		}
		return "decimal", nil
	case "double precision", "float8":
		return "float", nil
	case "real", "float4":
		return "real", nil
	default:
		return typ.Name, nil
	}
}

func quote(name string) string { return mssql.Dialect{}.EscapeName(name) }

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
