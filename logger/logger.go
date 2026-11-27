// Package logger is the query log from drizzle-orm.
//
// A nil Logger writes nothing. Default writes one line,
// "Query: <sql> -- params: [...]". Nop is the explicit no-op.
// DurationLogger is optional. Timed writes the query line and a duration line.
package logger

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
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

// DurationLogger is an optional addition to Logger.
// A logger that implements it receives the elapsed time after the driver returns, including when the call fails.
type DurationLogger interface {
	LogDuration(query string, params []any, elapsed time.Duration)
}

// Timed writes the query line and a duration line.
type Timed struct {
	Writer LogWriter
}

// LogQuery writes the same line as Default.
func (t Timed) LogQuery(query string, params []any) {
	Default{Writer: t.Writer}.LogQuery(query, params)
}

// LogDuration writes "Duration: <elapsed> Query: <sql>".
func (t Timed) LogDuration(query string, params []any, elapsed time.Duration) {
	w := t.Writer
	if w == nil {
		w = ConsoleWriter{}
	}
	w.Write("Duration: " + elapsed.String() + " Query: " + query + formatParams(params))
}

// Start writes the query line and returns a function that records the elapsed time.
// The returned function is safe to defer. A nil logger and a logger without LogDuration do not record a duration.
func Start(l Logger, query string, params []any) func() {
	Log(l, query, params)
	d, ok := l.(DurationLogger)
	if l == nil || !ok {
		return func() {}
	}
	began := time.Now()
	return func() {
		d.LogDuration(query, params, time.Since(began))
	}
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
