package logger_test

import (
	"strings"
	"testing"

	"github.com/go-mist/mist/logger"
)

type lines struct {
	got []string
}

func (l *lines) Write(message string) { l.got = append(l.got, message) }

func TestDefaultFormat(t *testing.T) {
	buf := &lines{}
	log := logger.Default{Writer: buf}
	log.LogQuery("select 1", nil)
	log.LogQuery("select $1", []any{"alice", 7})
	if len(buf.got) != 2 {
		t.Fatal(buf.got)
	}
	if buf.got[0] != "Query: select 1" {
		t.Fatal(buf.got[0])
	}
	if buf.got[1] != `Query: select $1 -- params: ["alice", 7]` {
		t.Fatal(buf.got[1])
	}
}

func TestNilAndNop(t *testing.T) {
	logger.Log(nil, "select 1", nil)
	logger.Nop{}.LogQuery("select 1", []any{1})
	done := logger.Start(nil, "select 1", nil)
	done()
}

func TestStartRecordsDuration(t *testing.T) {
	buf := &lines{}
	done := logger.Start(logger.Timed{Writer: buf}, "select 1", nil)
	done()
	if len(buf.got) != 2 {
		t.Fatal(buf.got)
	}
	if buf.got[0] != "Query: select 1" {
		t.Fatal(buf.got[0])
	}
	if !strings.HasPrefix(buf.got[1], "Duration: ") || !strings.Contains(buf.got[1], "Query: select 1") {
		t.Fatal(buf.got[1])
	}
}

func TestStartSkipsPlainLogger(t *testing.T) {
	buf := &lines{}
	done := logger.Start(logger.Default{Writer: buf}, "select 1", nil)
	done()
	if len(buf.got) != 1 || buf.got[0] != "Query: select 1" {
		t.Fatal(buf.got)
	}
}
