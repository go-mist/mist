package migrate

import (
	"strings"
	"testing"
)

func TestEngineSQL(t *testing.T) {
	pgEng, err := engineFor("pg")
	if err != nil {
		t.Fatal(err)
	}
	schema, table, err := pgEng.normalize(Config{})
	if err != nil || schema != "mist" || table != "__mist_migrations" {
		t.Fatalf("pg default = %s %s %v", schema, table, err)
	}
	got := pgEng.insertSQL("mist", "__mist_migrations")
	want := `insert into "mist"."__mist_migrations" ("hash", "created_at", "name") values ($1, $2, $3)`
	if got != want {
		t.Fatalf("pg insert = %s", got)
	}
	if got := pgEng.deleteSQL("mist", "__mist_migrations"); got != `delete from "mist"."__mist_migrations" where "name" = $1` {
		t.Fatalf("pg delete = %s", got)
	}
	if pgEng.createTable("mist", "__mist_migrations") != CreateTableSQL("mist", "__mist_migrations") {
		t.Fatal("pg create table changed")
	}

	my, err := engineFor("mysql")
	if err != nil {
		t.Fatal(err)
	}
	create := my.createTable("mist", "__mist_migrations")
	if !strings.Contains(create, "`mist`.`__mist_migrations`") || !strings.Contains(create, "AUTO_INCREMENT") {
		t.Fatalf("mysql create = %s", create)
	}
	if my.schemaSQL("mist") != "CREATE DATABASE IF NOT EXISTS `mist`" {
		t.Fatal(my.schemaSQL("mist"))
	}
	if stmt, _ := my.txLock("mist", "__mist_migrations"); stmt != "" {
		t.Fatalf("mysql lock moved into the transaction: %s", stmt)
	}

	lite, err := engineFor("sqlite")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := lite.normalize(Config{Schema: "mist"}); err == nil || !strings.Contains(err.Error(), `sqlite has no schema "mist"`) {
		t.Fatalf("schema error = %v", err)
	}
	schema, table, err = lite.normalize(Config{})
	if err != nil || schema != "main" || table != "__mist_migrations" {
		t.Fatalf("sqlite default = %s %s %v", schema, table, err)
	}
	if strings.Contains(lite.createTable(schema, table), "main.") {
		t.Fatal(lite.createTable(schema, table))
	}
	ora, err := engineFor("oracle")
	if err != nil {
		t.Fatal(err)
	}
	schema, table, err = ora.normalize(Config{})
	if err != nil || schema != "" || table != "__mist_migrations" {
		t.Fatalf("oracle default = %q %s %v", schema, table, err)
	}
	if strings.Contains(ora.createTable(schema, table), "AUTO_INCREMENT") || !strings.Contains(ora.createTable(schema, table), "IDENTITY") {
		t.Fatal(ora.createTable(schema, table))
	}
	if got := ora.insertSQL("", "__mist_migrations"); got != `insert into "__mist_migrations" ("hash", "created_at", "name") values (:1, :2, :3)` {
		t.Fatalf("oracle insert = %s", got)
	}
	if got := ora.appliedSQL("", "__mist_migrations"); got != `select "hash", "name" from "__mist_migrations"` {
		t.Fatalf("oracle applied = %s", got)
	}
	ms, err := engineFor("mssql")
	if err != nil {
		t.Fatal(err)
	}
	schema, table, err = ms.normalize(Config{})
	if err != nil || schema != "dbo" || table != "__mist_migrations" {
		t.Fatalf("mssql default = %q %s %v", schema, table, err)
	}
	if got := ms.insertSQL(schema, table); got != `insert into [dbo].[__mist_migrations] ([hash], [created_at], [name]) values (@p1, @p2, @p3)` {
		t.Fatalf("mssql insert = %s", got)
	}
	if !strings.Contains(ms.createTable(schema, table), "IDENTITY(1,1)") {
		t.Fatal(ms.createTable(schema, table))
	}
	if ms.schemaSQL("dbo") != "" {
		t.Fatal(ms.schemaSQL("dbo"))
	}
	if ora.schemaSQL("mist") != "" {
		t.Fatal(ora.schemaSQL("mist"))
	}
	dameng, err := engineFor("dm")
	if err != nil {
		t.Fatal(err)
	}
	schema, table, err = dameng.normalize(Config{})
	if err != nil || schema != "" || table != "__mist_migrations" {
		t.Fatalf("dm default = %q %s %v", schema, table, err)
	}
	if got := dameng.insertSQL("", "__mist_migrations"); got != `insert into "__mist_migrations" ("hash", "created_at", "name") values (?, ?, ?)` {
		t.Fatalf("dm insert = %s", got)
	}
	create = dameng.createTable("", "__mist_migrations")
	if !strings.Contains(create, `BIGINT IDENTITY(1,1)`) || strings.Contains(create, ".") {
		t.Fatal(create)
	}
	if dameng.schemaSQL("") != "" {
		t.Fatal(dameng.schemaSQL(""))
	}
	cols, args := dameng.columnsSQL("", "__mist_migrations")
	if !strings.Contains(cols, "table_name = ?") || len(args) != 1 {
		t.Fatalf("dm columns = %s %v", cols, args)
	}
}
