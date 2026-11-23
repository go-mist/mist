package op

import "github.com/go-mist/mist/sql"

// Add renders `(left + right)`.
func Add(left sql.Chunk, right any) *sql.SQL { return arith(left, " + ", right) }

// Sub renders `(left - right)`.
func Sub(left sql.Chunk, right any) *sql.SQL { return arith(left, " - ", right) }

// Mul renders `(left * right)`.
func Mul(left sql.Chunk, right any) *sql.SQL { return arith(left, " * ", right) }

// Div renders `(left / right)`.
func Div(left sql.Chunk, right any) *sql.SQL { return arith(left, " / ", right) }

func arith(left sql.Chunk, operator string, right any) *sql.SQL {
	return sql.S(sql.Fragment("("), left, sql.Fragment(operator), right, sql.Fragment(")"))
}
