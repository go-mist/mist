// Package sqlite renders diff changes as SQLite statements.
// On AlterColumn or AlterPrimaryKey, SQLite cannot ALTER in place. The table is rebuilt: create name__mist_new, copy the shared columns, drop the old table, and rename.
package sqlite

import (
	"fmt"
	"strings"

	"github.com/go-mist/mist/diff"
	"github.com/go-mist/mist/schema"
	mistsqlite "github.com/go-mist/mist/sqlite"
)

// breakpoint matches migrate's split marker verbatim.
const breakpoint = "--> statement-breakpoint"

// Render writes the changes as the body of migration.sql. want is the structure after the change and is used to rebuild the whole table when a column changes.
// No changes returns an empty string.
func Render(changes []diff.Change, want schema.Schema) (string, error) {
	stmts, err := statements(changes, want)
	if err != nil {
		return "", err
	}
	if len(stmts) == 0 {
		return "", nil
	}
	return strings.Join(stmts, "\n"+breakpoint+"\n") + "\n", nil
}

func statements(changes []diff.Change, want schema.Schema) ([]string, error) {
	rebuild := map[string]struct{}{}
	for _, change := range changes {
		if change.Kind == diff.AlterColumn || change.Kind == diff.AlterPrimaryKey {
			rebuild[change.Table] = struct{}{}
		}
	}
	rebuilt := map[string]struct{}{}
	var stmts []string
	for _, change := range changes {
		if _, ok := rebuild[change.Table]; ok && rebuildKind(change.Kind) {
			if _, done := rebuilt[change.Table]; done {
				continue
			}
			rebuilt[change.Table] = struct{}{}
			sql, err := rebuildTable(change.Table, changes, want)
			if err != nil {
				return nil, err
			}
			stmts = append(stmts, sql...)
			continue
		}
		sql, err := renderChange(change)
		if err != nil {
			return nil, err
		}
		stmts = append(stmts, sql...)
	}
	return stmts, nil
}

func rebuildKind(kind diff.Kind) bool {
	switch kind {
	case diff.AddColumn, diff.DropColumn, diff.AlterColumn, diff.AlterPrimaryKey,
		diff.AddUnique, diff.DropUnique, diff.AddIndex, diff.DropIndex,
		diff.AddCheck, diff.DropCheck, diff.AddForeignKey, diff.DropForeignKey:
		return true
	default:
		return false
	}
}

func rebuildTable(name string, changes []diff.Change, want schema.Schema) ([]string, error) {
	var table *schema.Table
	for i := range want.Tables {
		if want.Tables[i].Name == name {
			table = &want.Tables[i]
			break
		}
	}
	if table == nil {
		return nil, fmt.Errorf("sqlite: table %s must be rebuilt, but it is missing from the desired schema", name)
	}
	for _, col := range table.Columns {
		if col.Identity {
			return nil, fmt.Errorf("sqlite: table %s column %s uses identity, which SQLite does not have", name, col.Name)
		}
		if col.Type.Enum || col.Type.ArrayDims > 0 {
			return nil, fmt.Errorf("sqlite: table %s column %s cannot use enum or an array", name, col.Name)
		}
	}
	added := map[string]schema.Column{}
	for _, change := range changes {
		if change.Table != name || change.Kind != diff.AddColumn || change.Column == nil {
			continue
		}
		added[change.Column.Name] = *change.Column
	}
	var copyCols []string
	for _, col := range table.Columns {
		if col.Generated != "" {
			continue
		}
		if add, ok := added[col.Name]; ok {
			if add.NotNull && add.Default == "" {
				return nil, fmt.Errorf("sqlite: table %s added not-null column %s has no default, so existing rows cannot be filled", name, col.Name)
			}
			continue
		}
		copyCols = append(copyCols, col.Name)
	}
	if len(copyCols) == 0 {
		return nil, fmt.Errorf("sqlite: table %s rebuild has no columns to copy", name)
	}
	fresh := name + "__mist_new"
	create := createTableSQL(*table)
	create = strings.Replace(create, "CREATE TABLE "+quote(name)+" ", "CREATE TABLE "+quote(fresh)+" ", 1)
	var stmts []string
	stmts = append(stmts, create)
	stmts = append(stmts, "INSERT INTO "+quote(fresh)+" ("+quoteList(copyCols)+") SELECT "+quoteList(copyCols)+" FROM "+quote(name)+";")
	stmts = append(stmts, "DROP TABLE "+quote(name)+";")
	stmts = append(stmts, "ALTER TABLE "+quote(fresh)+" RENAME TO "+quote(name)+";")
	for _, idx := range table.Indexes {
		stmts = append(stmts, indexSQL(name, idx))
	}
	return stmts, nil
}

func renderChange(change diff.Change) ([]string, error) {
	switch change.Kind {
	case diff.CreateEnum, diff.DropEnum, diff.AlterEnum:
		name := ""
		if change.Enum != nil {
			name = change.Enum.Name
		}
		return nil, fmt.Errorf("sqlite: enum %s is not supported", name)
	case diff.CreateTable:
		if change.TableDef == nil {
			return nil, fmt.Errorf("sqlite: create table %s is missing a definition", change.Table)
		}
		var stmts []string
		stmts = append(stmts, createTableSQL(*change.TableDef))
		for _, idx := range change.TableDef.Indexes {
			stmts = append(stmts, indexSQL(change.TableDef.Name, idx))
		}
		return stmts, nil
	case diff.DropTable:
		return []string{"DROP TABLE " + quote(change.Table) + ";"}, nil
	case diff.AddColumn:
		if change.Column == nil {
			return nil, fmt.Errorf("sqlite: table %s add column is missing a definition", change.Table)
		}
		return []string{"ALTER TABLE " + quote(change.Table) + " ADD COLUMN " + columnSQL(*change.Column, false) + ";"}, nil
	case diff.DropColumn:
		return []string{"ALTER TABLE " + quote(change.Table) + " DROP COLUMN " + quote(change.Name) + ";"}, nil
	case diff.AddIndex:
		return []string{indexSQL(change.Table, *change.Index)}, nil
	case diff.DropIndex:
		return []string{"DROP INDEX " + quote(change.Name) + ";"}, nil
	case diff.AddUnique:
		return nil, fmt.Errorf("sqlite: table %s cannot add unique %s in place; a column or primary-key change rebuilds the table", change.Table, change.Unique.Name)
	case diff.DropUnique:
		return nil, fmt.Errorf("sqlite: table %s cannot drop unique %s in place", change.Table, change.Name)
	case diff.AddCheck, diff.DropCheck, diff.AddForeignKey, diff.DropForeignKey:
		return nil, fmt.Errorf("sqlite: table %s constraints cannot be changed in place; rebuild the table", change.Table)
	default:
		return nil, fmt.Errorf("sqlite: unknown change %s", change.Kind)
	}
}

func createTableSQL(table schema.Table) string {
	pkInline := len(table.PrimaryKey) == 1
	pkCol := ""
	if pkInline {
		pkCol = table.PrimaryKey[0]
	}
	var lines []string
	for _, col := range table.Columns {
		lines = append(lines, "\t"+columnSQL(col, pkInline && col.Name == pkCol))
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
	for _, fk := range table.ForeignKeys {
		lines = append(lines, "\t"+foreignKeySQL(fk))
	}
	return "CREATE TABLE " + quote(table.Name) + " (\n" + strings.Join(lines, ",\n") + "\n);"
}

func columnSQL(col schema.Column, primary bool) string {
	sql := quote(col.Name) + " " + renderType(col.Type)
	if col.Generated != "" {
		sql += " GENERATED ALWAYS AS (" + col.Generated + ") STORED"
	}
	if col.NotNull && !primary {
		sql += " NOT NULL"
	}
	if col.Default != "" && col.Generated == "" {
		sql += " DEFAULT " + col.Default
	}
	if primary {
		sql += " PRIMARY KEY"
	}
	return sql
}

func foreignKeySQL(fk schema.ForeignKey) string {
	sql := fmt.Sprintf("CONSTRAINT %s FOREIGN KEY (%s) REFERENCES %s (%s)",
		quote(fk.Name), quoteList(fk.Columns), quote(fk.RefTable), quoteList(fk.RefColumns))
	if fk.OnDelete != "" {
		sql += " ON DELETE " + fk.OnDelete
	}
	if fk.OnUpdate != "" {
		sql += " ON UPDATE " + fk.OnUpdate
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

func renderType(typ schema.Type) string {
	name := typ.Name
	switch name {
	case "varchar":
		if typ.Length > 0 {
			name = fmt.Sprintf("varchar(%d)", typ.Length)
		}
	case "numeric", "decimal":
		if typ.Precision > 0 && typ.Scale > 0 {
			name = fmt.Sprintf("%s(%d,%d)", name, typ.Precision, typ.Scale)
		} else if typ.Precision > 0 {
			name = fmt.Sprintf("%s(%d)", name, typ.Precision)
		}
	}
	return name
}

func quote(name string) string { return mistsqlite.Dialect{}.EscapeName(name) }

func quoteList(names []string) string {
	out := make([]string, len(names))
	for i, name := range names {
		out[i] = quote(name)
	}
	return strings.Join(out, ", ")
}
