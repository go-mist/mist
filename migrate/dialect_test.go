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
	got := pgEng.insertSQL("drizzle", "__drizzle_migrations")
	want := `insert into "drizzle"."__drizzle_migrations" ("hash", "created_at", "name") values ($1, $2, $3)`
	if got != want {
		t.Fatalf("pg insert = %s", got)
	}
	if pgEng.createTable("drizzle", "__drizzle_migrations") != CreateTableSQL("drizzle", "__drizzle_migrations") {
		t.Fatal("pg create table changed")
	}

	my, err := engineFor("mysql")
	if err != nil {
		t.Fatal(err)
	}
	create := my.createTable("drizzle", "__drizzle_migrations")
	if !strings.Contains(create, "`drizzle`.`__drizzle_migrations`") || !strings.Contains(create, "AUTO_INCREMENT") {
		t.Fatalf("mysql create = %s", create)
	}
	if my.schemaSQL("drizzle") != "CREATE DATABASE IF NOT EXISTS `drizzle`" {
		t.Fatal(my.schemaSQL("drizzle"))
	}
	if stmt, _ := my.txLock("drizzle", "__drizzle_migrations"); stmt != "" {
		t.Fatalf("mysql lock moved into the transaction: %s", stmt)
	}

	lite, err := engineFor("sqlite")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := lite.normalize(Config{Schema: "drizzle"}); err == nil || !strings.Contains(err.Error(), `sqlite has no schema "drizzle"`) {
		t.Fatalf("schema error = %v", err)
	}
	schema, table, err := lite.normalize(Config{})
	if err != nil || schema != "main" || table != "__drizzle_migrations" {
		t.Fatalf("sqlite default = %s %s %v", schema, table, err)
	}
	if strings.Contains(lite.createTable(schema, table), "main.") {
		t.Fatal(lite.createTable(schema, table))
	}
	if _, err := engineFor("oracle"); err == nil {
		t.Fatal("oracle is not a migrate driver yet")
	}
}
