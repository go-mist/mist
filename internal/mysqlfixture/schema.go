package mysqlfixture

import (
	"encoding/json"
	"time"

	"github.com/xbmlz/mist/mysql"
)

//go:generate go run github.com/xbmlz/mist/cmd/mist generate

// Note is a MySQL table. The generator should emit mysql.Col.
type Note struct {
	ID      int64           `drizzle:"id,bigint,pk"`
	Title   string          `drizzle:"title,varchar,notnull,length=64"`
	Body    json.RawMessage `drizzle:"body,json,notnull"`
	Amount  *string         `drizzle:"amount,decimal"`
	At      time.Time       `drizzle:"at,datetime,notnull"`
	Payload []byte          `drizzle:"payload,blob,notnull"`
	Flag    int8            `drizzle:"flag,tinyint,notnull"`
}

func (Note) Table() mysql.TableMeta { return mysql.TableMeta{Name: "m8_notes"} }
