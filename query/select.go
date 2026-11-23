// Package query assembles queries. It does not hold a connection.
//
// The dialect compiles a spec into SQL fragments through Build*Query, and sql.Render then emits text and parameters.
// All, One, and Exec hand the rendered result to Executor. Prepare renders once; later runs execute with new parameters.
// Connections and transactions both implement this interface.
package query

import (
	"github.com/xbmlz/mist/sql"
	"github.com/xbmlz/mist/sql/dialect"
)

// Column is one column in a select list. T is the row type.
// FieldPtr yields the field address during a scan. The generated column descriptor provides it.
// ColName is the column name without a table name. Insert column lists and the left-hand side of an update assignment use it.
type Column[T any] interface {
	sql.Chunk
	FieldPtr(*T) any
	ColName() string
}

// Table is a from source. Row binds the table to row type T, so From(Posts) cannot fit in Select[User].
// Scan is the generated whole-row scan function.
type Table[T any] interface {
	sql.Chunk
	Columns() []sql.Chunk
	Row() *T
	Scan(row sql.Row, dst *T) error
}

// SelectDialect is the capability a dialect must add to render one select.
// The three escape functions stay on dialect.Dialect. Build* is not pushed into that interface.
type SelectDialect interface {
	dialect.Dialect
	BuildSelectQuery(SelectSpec) (*sql.SQL, error)
}

// JoinKind is the kind of join. The dialect decides the keyword.
type JoinKind uint8

const (
	InnerJoin JoinKind = iota
	LeftJoin
	RightJoin
	FullJoin
	CrossJoin
)

// Join is one join. On is empty for CrossJoin.
type Join struct {
	Kind  JoinKind
	Table sql.Chunk
	On    sql.Chunk
}

// SelectSpec is the clauses of an expanded select chain. The dialect only reads it and does not modify query nodes.
//
// LimitSet false means the caller did not write Limit. Limit 0 with LimitSet true
// means LIMIT 0. Offset is the same. Joins are in call order.
type SelectSpec struct {
	Columns   []sql.Chunk
	From      sql.Chunk
	Joins     []Join
	Where     sql.Chunk
	GroupBy   []sql.Chunk
	Having    sql.Chunk
	OrderBy   []sql.Chunk
	Limit     int
	LimitSet  bool
	Offset    int
	OffsetSet bool
	// Sets are in call order. Ordering and limit / offset written after a set operation
	// are recorded on that operation and rendered after the whole result.
	Sets   []SetOp
	SetErr string
	// With is the CTEs in front of the statement, in call order. Outside a set operation they are written only once.
	With []WithSpec
	// When Distinct is true the select has distinct. A non-empty DistinctOn is distinct on.
	Distinct   bool
	DistinctOn []sql.Chunk
	// When Lock is non-nil the statement ends with for update or for share.
	Lock *RowLock
}

// RowLock is the row lock at the end of a select. An empty Of locks every table in the statement.
// NoWait and SkipLocked cannot both be true. Share is set by ForShare.
type RowLock struct {
	Of         []sql.Chunk
	NoWait     bool
	SkipLocked bool
	Share      bool
}

// WithSpec is one CTE. Query is its select spec.
type WithSpec struct {
	Name      string
	Recursive bool
	Query     SelectSpec
}

// WithSource is a query that can be placed in with. pg.CTE implements it.
type WithSource interface {
	WithSpec() WithSpec
}

// SetOp is one set operation. Right is the expanded spec of the query on the right.
type SetOp struct {
	Op        string
	Right     SelectSpec
	OrderBy   []sql.Chunk
	Limit     int
	LimitSet  bool
	Offset    int
	OffsetSet bool
}

type clauseKind uint8

const (
	clauseWhere clauseKind = iota
	clauseOrder
	clauseLimit
	clauseOffset
	clauseJoin
	clauseGroup
	clauseHaving
	clauseSet
	clauseWith
	clauseDistinct
	clauseLock
	clauseCount
)

// clause is one chained call. A new call points at the old node, so forks share the prefix.
type clause struct {
	prev   *clause
	kind   clauseKind
	join   JoinKind
	chunk  sql.Chunk
	on     sql.Chunk
	chunks []sql.Chunk
	n      int
	setOp  string
	right  SelectSpec
	withs  []WithSpec
	lock   *RowLock
}

// Select is an immutable select chain. Methods return a new value and do not change the chain from before the call.
type Select[T any] struct {
	picked   []Column[T]
	cols     []sql.Chunk
	table    Table[T]
	from     sql.Chunk
	fromCols []sql.Chunk
	tail     *clause
}

// GetSQL lets this select be placed inside another statement.
// The dialect replaces it with the real select before rendering. If it is not expanded, this comment stays in the text.
func (s Select[T]) GetSQL() *sql.SQL {
	return sql.Raw("/* query: subquery was not expanded */")
}

// QuerySpec hands this select to the dialect to expand into a scalar subquery.
func (s Select[T]) QuerySpec() SelectSpec { return s.Spec() }

// As gives this select a table alias and returns a derived table. Use it in From and Join.
func (s Select[T]) As(alias string) Subquery[T] {
	return Subquery[T]{alias: alias, spec: s.Spec(), cols: s.Projected()}
}

// Projected is the columns of this select. When no columns were written, it uses every column of the From table.
func (s Select[T]) Projected() []Column[T] {
	if len(s.picked) > 0 {
		return append([]Column[T](nil), s.picked...)
	}
	out := make([]Column[T], 0, len(s.fromCols))
	for _, ch := range s.fromCols {
		col, ok := ch.(Column[T])
		if ok {
			out = append(out, col)
		}
	}
	return out
}

// NewSelect starts a query. Passing no columns means the whole row; the column list is taken from the table at From.
// Callers write pg.Select, which forwards here. Go cannot give a function and a type the same name, so the type stays Select.
func NewSelect[T any](cols ...Column[T]) Select[T] {
	picked := append([]Column[T](nil), cols...)
	out := make([]sql.Chunk, len(picked))
	for i, col := range picked {
		out[i] = col
	}
	return Select[T]{picked: picked, cols: out}
}

// From sets the source table and fixes the row type as T.
func (s Select[T]) From(table Table[T]) Select[T] {
	s.table = table
	s.from = table
	if table != nil {
		s.fromCols = table.Columns()
	}
	return s
}

// Where sets the condition. A later call replaces the previous one and does not AND them automatically.
// A nil cond means this query has no where clause.
func (s Select[T]) Where(cond sql.Chunk) Select[T] {
	s.tail = &clause{prev: s.tail, kind: clauseWhere, chunk: cond}
	return s
}

// OrderBy sets the ordering. A later call replaces the previous one.
func (s Select[T]) OrderBy(exprs ...sql.Chunk) Select[T] {
	s.tail = &clause{prev: s.tail, kind: clauseOrder, chunks: append([]sql.Chunk(nil), exprs...)}
	return s
}

// Distinct makes the select use distinct. A later call replaces distinct on.
func (s Select[T]) Distinct() Select[T] {
	s.tail = &clause{prev: s.tail, kind: clauseDistinct}
	return s
}

// DistinctOn produces distinct on (cols). A later call replaces the previous one.
func (s Select[T]) DistinctOn(cols ...sql.Chunk) Select[T] {
	s.tail = &clause{prev: s.tail, kind: clauseDistinct, chunks: append([]sql.Chunk{}, cols...)}
	return s
}

// ForUpdate adds for update at the end of the statement. Passing no option locks every table.
// A later call replaces the previous one, including ForShare.
func (s Select[T]) ForUpdate(locks ...RowLock) Select[T] {
	return s.forLock(false, locks...)
}

// ForShare adds for share at the end of the statement. Passing no option locks every table.
// A later call replaces the previous one, including ForUpdate.
func (s Select[T]) ForShare(locks ...RowLock) Select[T] {
	return s.forLock(true, locks...)
}

func (s Select[T]) forLock(share bool, locks ...RowLock) Select[T] {
	lock := RowLock{Share: share}
	if len(locks) == 1 {
		lock = locks[0]
		lock.Share = share
		lock.Of = append([]sql.Chunk(nil), locks[0].Of...)
	}
	s.tail = &clause{prev: s.tail, kind: clauseLock, n: len(locks), lock: &lock}
	return s
}

// Limit sets the limit. Limit(0) renders as LIMIT 0.
func (s Select[T]) Limit(n int) Select[T] {
	s.tail = &clause{prev: s.tail, kind: clauseLimit, n: n}
	return s
}

// Offset sets the offset. Offset(0) renders as OFFSET 0.
func (s Select[T]) Offset(n int) Select[T] {
	s.tail = &clause{prev: s.tail, kind: clauseOffset, n: n}
	return s
}

// GroupBy sets the grouping. A later call replaces the previous one. Passing no expressions means there is no group by.
func (s Select[T]) GroupBy(exprs ...sql.Chunk) Select[T] {
	s.tail = &clause{prev: s.tail, kind: clauseGroup, chunks: append([]sql.Chunk(nil), exprs...)}
	return s
}

// Having sets the condition after grouping. A later call replaces the previous one.
// A nil cond means this query has no having clause.
func (s Select[T]) Having(cond sql.Chunk) Select[T] {
	s.tail = &clause{prev: s.tail, kind: clauseHaving, chunk: cond}
	return s
}

// Union appends union and keeps one copy of duplicate rows. Column names and order must match on both sides.
func (s Select[T]) Union(other Select[T]) Select[T] { return s.addSet("union", other) }

// UnionAll appends union all and keeps duplicate rows.
func (s Select[T]) UnionAll(other Select[T]) Select[T] { return s.addSet("union all", other) }

// Intersect appends intersect and keeps only rows present on both sides.
func (s Select[T]) Intersect(other Select[T]) Select[T] { return s.addSet("intersect", other) }

// IntersectAll appends intersect all.
func (s Select[T]) IntersectAll(other Select[T]) Select[T] {
	return s.addSet("intersect all", other)
}

// Except appends except and keeps rows the left side has that the right side does not.
func (s Select[T]) Except(other Select[T]) Select[T] { return s.addSet("except", other) }

// ExceptAll appends except all.
func (s Select[T]) ExceptAll(other Select[T]) Select[T] { return s.addSet("except all", other) }

func (s Select[T]) addSet(op string, other Select[T]) Select[T] {
	s.tail = &clause{prev: s.tail, kind: clauseSet, setOp: op, right: other.Spec()}
	return s
}

// With adds CTEs in front of the statement. Repeated calls keep them in the order written.
// If any CTE is recursive, the keyword is with recursive.
func (s Select[T]) With(ctes ...WithSource) Select[T] {
	s.tail = &clause{prev: s.tail, kind: clauseWith, withs: snapshotWith(ctes)}
	return s
}

func snapshotWith(ctes []WithSource) []WithSpec {
	specs := make([]WithSpec, len(ctes))
	for i, cte := range ctes {
		if cte != nil {
			specs[i] = cte.WithSpec()
		}
	}
	return specs
}

func appendWith(prev []WithSpec, ctes ...WithSource) []WithSpec {
	return append(append([]WithSpec(nil), prev...), snapshotWith(ctes)...)
}

// InnerJoin appends an inner join. Repeated calls keep them in the order written and do not replace one another.
//
// The row type stays the T fixed by From, and the select list can only be columns of that table.
// A Go method cannot declare further type parameters, so a joined table only has to render as SQL.
func (s Select[T]) InnerJoin(table sql.Chunk, on sql.Chunk) Select[T] {
	return s.addJoin(InnerJoin, table, on)
}

// LeftJoin appends a left join.
func (s Select[T]) LeftJoin(table sql.Chunk, on sql.Chunk) Select[T] {
	return s.addJoin(LeftJoin, table, on)
}

// RightJoin appends a right join.
func (s Select[T]) RightJoin(table sql.Chunk, on sql.Chunk) Select[T] {
	return s.addJoin(RightJoin, table, on)
}

// FullJoin appends a full join.
func (s Select[T]) FullJoin(table sql.Chunk, on sql.Chunk) Select[T] {
	return s.addJoin(FullJoin, table, on)
}

// CrossJoin appends a cross join, with no on.
func (s Select[T]) CrossJoin(table sql.Chunk) Select[T] {
	return s.addJoin(CrossJoin, table, nil)
}

func (s Select[T]) addJoin(kind JoinKind, table, on sql.Chunk) Select[T] {
	s.tail = &clause{prev: s.tail, kind: clauseJoin, join: kind, chunk: table, on: on}
	return s
}

// Spec expands this chain. For Where, GroupBy, Having, OrderBy, Limit, and Offset, the later write wins.
// Joins and set operations are all kept and restored to call order.
// Ordering and limit written before a set operation stay on the left-hand query.
// Anything written after one particular set operation is recorded on that operation.
func (s Select[T]) Spec() SelectSpec {
	spec := foldSelect(s.from, s.cols, s.tail)
	if len(spec.Columns) == 0 {
		spec.Columns = s.fromCols
	}
	return spec
}

func foldSelect(from sql.Chunk, cols []sql.Chunk, tail *clause) SelectSpec {
	spec := SelectSpec{Columns: cols, From: from}
	for _, n := range callOrder(tail) {
		if n.kind == clauseSet {
			spec.Sets = append(spec.Sets, SetOp{Op: n.setOp, Right: n.right})
			continue
		}
		if n.kind == clauseWith {
			spec.With = append(spec.With, n.withs...)
			continue
		}
		if len(spec.Sets) > 0 {
			last := &spec.Sets[len(spec.Sets)-1]
			switch n.kind {
			case clauseOrder:
				last.OrderBy = n.chunks
			case clauseLimit:
				last.Limit = n.n
				last.LimitSet = true
			case clauseOffset:
				last.Offset = n.n
				last.OffsetSet = true
			default:
				if spec.SetErr == "" {
					spec.SetErr = "query: where, group by, having, join, distinct, or for update cannot follow a set operation"
				}
			}
			continue
		}
		switch n.kind {
		case clauseJoin:
			spec.Joins = append(spec.Joins, Join{Kind: n.join, Table: n.chunk, On: n.on})
		case clauseWhere:
			spec.Where = n.chunk
		case clauseGroup:
			spec.GroupBy = n.chunks
		case clauseHaving:
			spec.Having = n.chunk
		case clauseOrder:
			spec.OrderBy = n.chunks
		case clauseLimit:
			spec.Limit = n.n
			spec.LimitSet = true
		case clauseOffset:
			spec.Offset = n.n
			spec.OffsetSet = true
		case clauseDistinct:
			spec.Distinct = true
			spec.DistinctOn = n.chunks
		case clauseLock:
			if n.n > 1 && spec.SetErr == "" {
				spec.SetErr = "query: row lock accepts only one option"
			}
			spec.Lock = n.lock
		}
	}
	return spec
}

func callOrder(tail *clause) []*clause {
	list := make([]*clause, 0)
	for n := tail; n != nil; n = n.prev {
		list = append(list, n)
	}
	for i, j := 0, len(list)-1; i < j; i, j = i+1, j-1 {
		list[i], list[j] = list[j], list[i]
	}
	return list
}

func buildSpec(from sql.Chunk, cols []sql.Chunk, tail *clause) SelectSpec {
	spec := SelectSpec{Columns: cols, From: from}
	var seen [clauseCount]bool
	var joins []Join
	for n := tail; n != nil; n = n.prev {
		if n.kind == clauseJoin {
			joins = append(joins, Join{Kind: n.join, Table: n.chunk, On: n.on})
			continue
		}
		if seen[n.kind] {
			continue
		}
		seen[n.kind] = true
		switch n.kind {
		case clauseWhere:
			spec.Where = n.chunk
		case clauseGroup:
			spec.GroupBy = n.chunks
		case clauseHaving:
			spec.Having = n.chunk
		case clauseOrder:
			spec.OrderBy = n.chunks
		case clauseLimit:
			spec.Limit = n.n
			spec.LimitSet = true
		case clauseOffset:
			spec.Offset = n.n
			spec.OffsetSet = true
		}
	}
	for i, j := 0, len(joins)-1; i < j; i, j = i+1, j-1 {
		joins[i], joins[j] = joins[j], joins[i]
	}
	spec.Joins = joins
	return spec
}

// SQL asks the dialect to build the fragments, then renders them into text and parameters.
func (s Select[T]) SQL(d SelectDialect) (sql.Query, error) {
	built, err := d.BuildSelectQuery(s.Spec())
	if err != nil {
		return sql.Query{}, err
	}
	return sql.Render(built, d, sql.RenderConfig{})
}
