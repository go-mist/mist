// Package mysql renders diff changes as MySQL statements.
package mysql

import (
	"fmt"
	"strings"

	"github.com/go-mist/mist/ddl"
	"github.com/go-mist/mist/dialect/mysql"
	"github.com/go-mist/mist/diff"
	"github.com/go-mist/mist/schema"
)

const breakpoint = "--> statement-breakpoint"

// Render writes the changes as the body of one up.sql. No changes returns an empty string.
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
	var stmts []string
	for _, change := range rest {
		if change.Kind == diff.CreateEnum || change.Kind == diff.DropEnum || change.Kind == diff.AlterEnum {
			return nil, fmt.Errorf("mysql: enum %s is not a CREATE TYPE%s", enumName(change), at(change.Loc))
		}
	}
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
	stmts = append(stmts, dropped...)
	return stmts, nil
}

func renderChange(change diff.Change) ([]string, error) {
	switch change.Kind {
	case diff.AddColumn:
		line, err := columnSQLChecked(*change.Column, false)
		if err != nil {
			return nil, err
		}
		return []string{"ALTER TABLE " + quote(change.Table) + " ADD COLUMN " + line + ";"}, nil
	case diff.DropColumn:
		return []string{"ALTER TABLE " + quote(change.Table) + " DROP COLUMN " + quote(change.Name) + ";"}, nil
	case diff.AlterColumn:
		return alterColumn(change)
	case diff.AlterPrimaryKey:
		return alterPrimaryKey(change), nil
	case diff.AddUnique:
		return []string{fmt.Sprintf("ALTER TABLE %s ADD CONSTRAINT %s UNIQUE (%s);", quote(change.Table), quote(change.Unique.Name), quoteList(change.Unique.Columns))}, nil
	case diff.DropUnique, diff.DropCheck, diff.DropForeignKey:
		return []string{"ALTER TABLE " + quote(change.Table) + " DROP CONSTRAINT " + quote(change.Name) + ";"}, nil
	case diff.AddIndex:
		return []string{indexSQL(change.Table, *change.Index)}, nil
	case diff.DropIndex:
		return []string{"DROP INDEX " + quote(change.Name) + " ON " + quote(change.Table) + ";"}, nil
	case diff.AddCheck:
		return []string{fmt.Sprintf("ALTER TABLE %s ADD CONSTRAINT %s CHECK (%s);", quote(change.Table), quote(change.Check.Name), change.Check.Expr)}, nil
	case diff.AddForeignKey:
		return []string{"ALTER TABLE " + quote(change.Table) + " ADD " + foreignKeySQL(*change.ForeignKey) + ";"}, nil
	default:
		return nil, fmt.Errorf("mysql: unknown change %s", change.Kind)
	}
}

func alterColumn(change diff.Change) ([]string, error) {
	from, to := change.ColumnFrom, change.Column
	if from.Identity != to.Identity || from.Generated != to.Generated {
		return nil, fmt.Errorf("mysql: column %s.%s identity or generated expression cannot be altered in place%s", change.Table, to.Name, at(change.Loc))
	}
	sql, err := columnSQLChecked(*to, false)
	if err != nil {
		return nil, err
	}
	return []string{"ALTER TABLE " + quote(change.Table) + " MODIFY COLUMN " + sql + ";"}, nil
}

func alterPrimaryKey(change diff.Change) []string {
	var stmts []string
	if len(change.PrimaryKeyFrom) > 0 {
		stmts = append(stmts, "ALTER TABLE "+quote(change.Table)+" DROP PRIMARY KEY;")
	}
	if len(change.PrimaryKeyTo) > 0 {
		stmts = append(stmts, "ALTER TABLE "+quote(change.Table)+" ADD PRIMARY KEY ("+quoteList(change.PrimaryKeyTo)+");")
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
			stmts = append(stmts, "ALTER TABLE "+quote(table.Name)+" ADD "+foreignKeySQL(fk)+";")
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
		stmts = append(stmts, "DROP TABLE "+quote(ordered[i].Name)+";")
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
		line, err := columnSQLChecked(col, pkInline && col.Name == pkCol)
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
	return "CREATE TABLE " + quote(table.Name) + " (\n" + strings.Join(lines, ",\n") + "\n);", nil
}

func columnSQLChecked(col schema.Column, primary bool) (string, error) {
	typ, err := renderType(col.Type)
	if err != nil {
		return "", fmt.Errorf("mysql: column %s: %w", col.Name, err)
	}
	sql := quote(col.Name) + " " + typ
	if col.Generated != "" {
		sql += " GENERATED ALWAYS AS (" + col.Generated + ") STORED"
	}
	if col.NotNull && !primary {
		sql += " NOT NULL"
	}
	if col.Default != "" && col.Generated == "" && !col.Identity {
		sql += " DEFAULT " + col.Default
	}
	if col.Identity {
		sql += " AUTO_INCREMENT"
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
	return "CREATE " + unique + "INDEX " + quote(idx.Name) + " ON " + quote(table) + " (" + quoteList(idx.Columns) + ");"
}

func renderType(typ schema.Type) (string, error) {
	if typ.Enum || typ.ArrayDims > 0 {
		return "", fmt.Errorf("type %s is not supported", typ.Name)
	}
	switch typ.Name {
	case "boolean", "bool", "timestamptz", "jsonb", "bytea", "uuid", "bigserial", "serial", "smallserial":
		return "", fmt.Errorf("type %s is not supported", typ.Name)
	case "varchar", "char":
		if typ.Length > 0 {
			return fmt.Sprintf("%s(%d)", typ.Name, typ.Length), nil
		}
		return typ.Name, nil
	case "numeric", "decimal":
		if typ.Precision > 0 && typ.Scale > 0 {
			return fmt.Sprintf("%s(%d,%d)", typ.Name, typ.Precision, typ.Scale), nil
		}
		if typ.Precision > 0 {
			return fmt.Sprintf("%s(%d)", typ.Name, typ.Precision), nil
		}
		return typ.Name, nil
	default:
		return typ.Name, nil
	}
}

func quote(name string) string { return mysql.Dialect{}.EscapeName(name) }

func quoteList(names []string) string {
	parts := make([]string, len(names))
	for i, name := range names {
		parts[i] = quote(name)
	}
	return strings.Join(parts, ", ")
}

func enumName(change diff.Change) string {
	if change.Enum != nil {
		return change.Enum.Name
	}
	return ""
}

func at(loc schema.Loc) string {
	if loc.File == "" {
		return ""
	}
	return fmt.Sprintf(" (%s:%d)", loc.File, loc.Line)
}
