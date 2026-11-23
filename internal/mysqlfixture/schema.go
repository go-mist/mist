package mysqlfixture

import (
	"encoding/json"
	"time"

	"github.com/go-mist/mist/mysql"
)

//go:generate go run github.com/go-mist/mist/cmd/mist generate

// Note is a MySQL table. The generator should emit mysql.Col.
type Note struct {
	ID      int64           `mist:"id,bigint,pk"`
	Title   string          `mist:"title,varchar,notnull,length=64"`
	Body    json.RawMessage `mist:"body,json,notnull"`
	Amount  *string         `mist:"amount,decimal"`
	At      time.Time       `mist:"at,datetime,notnull"`
	Payload []byte          `mist:"payload,blob,notnull"`
	Flag    int8            `mist:"flag,tinyint,notnull"`
}

func (Note) Table() mysql.TableMeta { return mysql.TableMeta{Name: "m8_notes"} }
