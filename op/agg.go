package op

import "github.com/xbmlz/mist/sql"

// Count renders count. With no expression it is count(*); with one expression it is count(expr).
func Count(expr ...sql.Chunk) *sql.SQL {
	if len(expr) == 0 || expr[0] == nil {
		return sql.Raw("count(*)")
	}
	return call("count", false, expr...)
}

// CountDistinct renders count(distinct expr).
func CountDistinct(expr sql.Chunk) *sql.SQL {
	return call("count", true, expr)
}

// Sum renders sum(expr).
func Sum(expr sql.Chunk) *sql.SQL {
	return call("sum", false, expr)
}

// SumDistinct renders sum(distinct expr).
func SumDistinct(expr sql.Chunk) *sql.SQL {
	return call("sum", true, expr)
}

// Avg renders avg(expr).
func Avg(expr sql.Chunk) *sql.SQL {
	return call("avg", false, expr)
}

// AvgDistinct renders avg(distinct expr).
func AvgDistinct(expr sql.Chunk) *sql.SQL {
	return call("avg", true, expr)
}

// Min renders min(expr).
func Min(expr sql.Chunk) *sql.SQL {
	return call("min", false, expr)
}

// Max renders max(expr).
func Max(expr sql.Chunk) *sql.SQL {
	return call("max", false, expr)
}

func call(fn string, distinct bool, expr ...sql.Chunk) *sql.SQL {
	if distinct {
		return sql.S(sql.Fragment(fn+"(distinct "), sql.Join(exprs(expr), sql.Fragment(", ")), sql.Fragment(")"))
	}
	return sql.S(sql.Fragment(fn+"("), sql.Join(exprs(expr), sql.Fragment(", ")), sql.Fragment(")"))
}

func exprs(expr []sql.Chunk) []sql.Chunk {
	out := make([]sql.Chunk, 0, len(expr))
	for _, e := range expr {
		if e != nil {
			out = append(out, e)
		}
	}
	return out
}
