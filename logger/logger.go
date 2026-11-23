// Package logger is the query log from drizzle-orm.
//
// A nil Logger writes nothing. Default writes one line,
// "Query: <sql> -- params: [...]". Nop is the explicit no-op.
// Slow queries and tracing are not this package.
package logger

import (
	"encoding/json"
	"fmt"
	"strings"
)

// Logger receives the SQL text and the bound parameters before the driver runs them.
type Logger interface {
	LogQuery(query string, params []any)
}

// LogWriter is where Default sends the formatted line.
type LogWriter interface {
	Write(message string)
}

// ConsoleWriter writes a line to standard output.
type ConsoleWriter struct{}

// Write prints message and a newline.
func (ConsoleWriter) Write(message string) { fmt.Println(message) }

// Default formats a query the same way as drizzle-orm's DefaultLogger.
// A nil Writer uses ConsoleWriter.
type Default struct {
	Writer LogWriter
}

// LogQuery writes "Query: ..." and, when params is non-empty, " -- params: [...]".
func (d Default) LogQuery(query string, params []any) {
	w := d.Writer
	if w == nil {
		w = ConsoleWriter{}
	}
	w.Write("Query: " + query + formatParams(params))
}

// Nop drops every query.
type Nop struct{}

// LogQuery does nothing.
func (Nop) LogQuery(string, []any) {}

// Log calls l when it is not nil.
func Log(l Logger, query string, params []any) {
	if l == nil {
		return
	}
	l.LogQuery(query, params)
}

func formatParams(params []any) string {
	if len(params) == 0 {
		return ""
	}
	parts := make([]string, len(params))
	for i, param := range params {
		encoded, err := json.Marshal(param)
		if err != nil {
			parts[i] = fmt.Sprint(param)
			continue
		}
		parts[i] = string(encoded)
	}
	return " -- params: [" + strings.Join(parts, ", ") + "]"
}
