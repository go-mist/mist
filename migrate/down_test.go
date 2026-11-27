package migrate

import (
	"strings"
	"testing"
	"testing/fstest"
)

func TestDownPlan(t *testing.T) {
	rows := []appliedRow{{Name: "a", Hash: "1"}, {Name: "c", Hash: "3"}, {Name: "b", Hash: "2"}}
	latest, err := downPlan(rows, downLatest)
	if err != nil || len(latest) != 1 || latest[0] != "c" {
		t.Fatalf("latest = %v %v", latest, err)
	}
	newer, err := downPlan(rows, "a")
	if err != nil || strings.Join(newer, ",") != "c,b" {
		t.Fatalf("to a = %v %v", newer, err)
	}
	all, err := downPlan(rows, "")
	if err != nil || strings.Join(all, ",") != "c,b,a" {
		t.Fatalf("all = %v %v", all, err)
	}
	if _, err := downPlan(rows, "missing"); err == nil || !strings.Contains(err.Error(), "not applied") {
		t.Fatal(err)
	}
	none, err := downPlan(nil, downLatest)
	if err != nil || none != nil {
		t.Fatalf("empty = %v %v", none, err)
	}
}

func TestDownStatementsRejectsPlaceholder(t *testing.T) {
	files := fstest.MapFS{
		"20240101000000_init/down.sql": {Data: []byte(DownPlaceholder)},
		"20240102000000_next/down.sql": {Data: []byte("-- only a comment\n")},
		"20240103000000_real/down.sql": {Data: []byte("drop table tasks;\n--> statement-breakpoint\n-- note\ndrop table events;\n")},
	}
	if _, err := downStatements(files, "20240101000000_init"); err == nil || !strings.Contains(err.Error(), "no down.sql") {
		t.Fatal(err)
	}
	if _, err := downStatements(files, "20240102000000_next"); err == nil {
		t.Fatal("comment-only down was accepted")
	}
	got, err := downStatements(files, "20240103000000_real")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0] != "drop table tasks;" || !strings.Contains(got[1], "drop table events;") {
		t.Fatalf("%#v", got)
	}
	if _, err := downStatements(files, "20240104000000_gone"); err == nil {
		t.Fatal("missing down was accepted")
	}
}
