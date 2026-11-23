package query

import (
	"reflect"

	"github.com/xbmlz/mist/sql"
	"github.com/xbmlz/mist/sql/dialect"
)

// InsertDialect is the capability a dialect must add to render an insert.
type InsertDialect interface {
	dialect.Dialect
	BuildInsertQuery(InsertSpec) (*sql.SQL, error)
}

// UpdateDialect is the capability a dialect must add to render an update.
type UpdateDialect interface {
	dialect.Dialect
	BuildUpdateQuery(UpdateSpec) (*sql.SQL, error)
}

// DeleteDialect is the capability a dialect must add to render a delete.
type DeleteDialect interface {
	dialect.Dialect
	BuildDeleteQuery(DeleteSpec) (*sql.SQL, error)
}

// InsertConflict is an on conflict clause. When Nothing is true, Sets are ignored.
// TargetWhere is written after the conflict target. SetWhere is written after do update set.
type InsertConflict struct {
	Nothing     bool
	Targets     []string
	TargetWhere sql.Chunk
	Sets        []Assignment
	SetWhere    sql.Chunk
}

// InsertSpec is an insert chain after expansion. A nil in Rows is SQL NULL.
// A nil Conflict means there is no on conflict.
// When DuplicateKey is true the assignments are in Duplicate. That is MySQL on duplicate key update, not on conflict.
// When Ignore is true the statement is written as insert ignore, not on conflict do nothing.
type InsertSpec struct {
	Table        sql.Chunk
	ColumnNames  []string
	Encoders     []sql.Encoder
	Rows         [][]any
	Conflict     *InsertConflict
	DuplicateKey bool
	Ignore       bool
	Duplicate    []Assignment
	Returning    []sql.Chunk
	With         []WithSpec
	// When Select is non-nil the statement is insert ... select and values are not written.
	Select *SelectSpec
}

// UpdateSpec is an update chain after expansion. Sets and Joins are in call order.
// The later From wins.
// OrderBy and Limit are single-table MySQL order by / limit. LimitSet false means Limit was not written.
type UpdateSpec struct {
	Table     sql.Chunk
	Sets      []Assignment
	From      sql.Chunk
	Joins     []Join
	Where     sql.Chunk
	OrderBy   []sql.Chunk
	Limit     int
	LimitSet  bool
	Returning []sql.Chunk
	With      []WithSpec
}

// Assignment is one entry in a set. A nil Value binds NULL. An SQL fragment is rendered as written.
type Assignment struct {
	Column  string
	Value   any
	Encoder sql.Encoder
}

// DeleteSpec is a delete chain after expansion. An empty Where means there is no where clause.
// The later Using wins. Joins are in call order.
// OrderBy and Limit are single-table MySQL order by / limit. LimitSet false means Limit was not written.
type DeleteSpec struct {
	Table     sql.Chunk
	Using     sql.Chunk
	Joins     []Join
	Where     sql.Chunk
	OrderBy   []sql.Chunk
	Limit     int
	LimitSet  bool
	Returning []sql.Chunk
	With      []WithSpec
}

type rowNode struct {
	prev *rowNode
	vals []any
}

// Insert is an immutable insert chain.
type Insert[T any] struct {
	table Table[T]
	// colBuf and oneRow keep a single small insert on the stack.
	// cols and tail are used when the statement is wider or has more than one row.
	colBuf        [4]Column[T]
	oneRow        [4]any
	nCols         int
	nVals         int
	one           bool
	cols          []Column[T]
	tail          *rowNode
	nothing       bool
	duplicate     bool
	ignore        bool
	conflict      []Column[T]
	conflictWhere sql.Chunk
	sets          *setNode[T]
	setWhere      sql.Chunk
	returning     []Column[T]
	withs         []WithSpec
	sel           *SelectSpec
}

// NewInsert starts an insert. Callers write pg.Insert.
func NewInsert[T any](table Table[T]) Insert[T] {
	return Insert[T]{table: table}
}

// Columns names the columns to write. A later call replaces the previous one.
// Columns that do not appear here are omitted from the statement and left to the database default. Zero values in the column list are written as given.
func (ins Insert[T]) Columns(cols ...Column[T]) Insert[T] {
	if len(cols) <= len(ins.colBuf) {
		for i, col := range cols {
			ins.colBuf[i] = col
		}
		for i := len(cols); i < len(ins.colBuf); i++ {
			ins.colBuf[i] = nil
		}
		ins.nCols = len(cols)
		ins.cols = nil
		return ins
	}
	ins.cols = append([]Column[T](nil), cols...)
	ins.nCols = len(cols)
	return ins
}

// SelectSource is a query that can be placed in insert ... select.
type SelectSource interface {
	QuerySpec() SelectSpec
}

// Select switches to insert ... select. The column count must match Columns. A later call replaces the previous one.
// It cannot be used together with Values.
func (ins Insert[T]) Select(q SelectSource) Insert[T] {
	if q == nil {
		ins.sel = nil
		return ins
	}
	spec := q.QuerySpec()
	ins.sel = &spec
	return ins
}

// Values appends one row. The number of values must match Columns. Repeated calls combine into one multi-row insert.
// A cell may be a select, rendered as a scalar subquery.
func (ins Insert[T]) Values(vals ...any) Insert[T] {
	if ins.tail == nil && !ins.one && len(vals) <= len(ins.oneRow) {
		for i, v := range vals {
			ins.oneRow[i] = nullValue(v)
		}
		ins.nVals = len(vals)
		ins.one = true
		return ins
	}
	if ins.one {
		copied := make([]any, ins.nVals)
		copy(copied, ins.oneRow[:ins.nVals])
		ins.tail = &rowNode{vals: copied}
		ins.one = false
		ins.nVals = 0
	}
	copied := make([]any, len(vals))
	for i, v := range vals {
		copied[i] = nullValue(v)
	}
	ins.tail = &rowNode{prev: ins.tail, vals: copied}
	return ins
}

// OnConflictDoNothing adds on conflict do nothing.
// Columns passed in are the conflict target. Passing none leaves the target unspecified. A later call replaces the target.
func (ins Insert[T]) OnConflictDoNothing(targets ...Column[T]) Insert[T] {
	ins.nothing = true
	ins.conflict = append([]Column[T](nil), targets...)
	return ins
}

// OnConflictDoUpdate updates on conflict. targets are the conflict-target columns. Assignments come from a following Set.
// A later call replaces the target. Set still accumulates.
func (ins Insert[T]) OnConflictDoUpdate(targets ...Column[T]) Insert[T] {
	ins.nothing = false
	ins.conflict = append([]Column[T](nil), targets...)
	return ins
}

// OnConflictWhere sets the where on the conflict target, the predicate of a partial index.
// A later call replaces the previous one.
func (ins Insert[T]) OnConflictWhere(cond sql.Chunk) Insert[T] {
	ins.conflictWhere = cond
	return ins
}

// Set appends one assignment of on conflict do update. Calling it again on the same column writes that column once more.
// Zero values are written as given. nil binds NULL. An SQL fragment enters the statement as written.
func (ins Insert[T]) Set(col Column[T], value any) Insert[T] {
	ins.sets = &setNode[T]{prev: ins.sets, col: col, val: nullValue(value)}
	return ins
}

// UpdateWhere sets the where after do update. A later call replaces the previous one.
// When the condition does not hold, the conflicting row stays unchanged.
func (ins Insert[T]) UpdateWhere(cond sql.Chunk) Insert[T] {
	ins.setWhere = cond
	return ins
}

// OnDuplicateKeyUpdate marks an update on a unique-key conflict. Assignments come from Set.
// There is no target column and no where. This is not on conflict.
func (ins Insert[T]) OnDuplicateKeyUpdate() Insert[T] {
	ins.duplicate = true
	return ins
}

// Ignore writes insert ignore. On a unique-key conflict the new row is dropped, with no error and no change to the old row.
// This is not on conflict do nothing.
func (ins Insert[T]) Ignore() Insert[T] {
	ins.ignore = true
	return ins
}

// With adds CTEs in front of the statement. Repeated calls keep them in the order written.
func (ins Insert[T]) With(ctes ...WithSource) Insert[T] {
	ins.withs = appendWith(ins.withs, ctes...)
	return ins
}

// Returning sets RETURNING. A later call replaces the previous one.
func (ins Insert[T]) Returning(cols ...Column[T]) Insert[T] {
	ins.returning = append([]Column[T](nil), cols...)
	return ins
}

// Spec expands this chain. Values are restored to call order.
func (ins Insert[T]) Spec() InsertSpec {
	cols := ins.cols
	if cols == nil && ins.nCols > 0 {
		cols = make([]Column[T], ins.nCols)
		copy(cols, ins.colBuf[:ins.nCols])
	}
	spec := InsertSpec{
		Table:       ins.table,
		ColumnNames: columnNames(cols),
		Encoders:    columnEncoders(cols),
		Returning:   columnChunks(ins.returning),
		With:        append([]WithSpec(nil), ins.withs...),
		Select:      ins.sel,
		Ignore:      ins.ignore,
	}
	if ins.duplicate {
		spec.DuplicateKey = true
		spec.Duplicate = assignments(ins.sets)
	}
	if ins.nothing || len(ins.conflict) > 0 || (!ins.duplicate && ins.sets != nil) || ins.conflictWhere != nil || ins.setWhere != nil {
		spec.Conflict = &InsertConflict{
			Nothing:     ins.nothing,
			Targets:     columnNames(ins.conflict),
			TargetWhere: ins.conflictWhere,
			Sets:        assignments(ins.sets),
			SetWhere:    ins.setWhere,
		}
	}
	var rows [][]any
	if ins.one {
		row := make([]any, ins.nVals)
		copy(row, ins.oneRow[:ins.nVals])
		rows = append(rows, row)
	}
	for n := ins.tail; n != nil; n = n.prev {
		rows = append(rows, n.vals)
	}
	for i, j := 0, len(rows)-1; i < j; i, j = i+1, j-1 {
		rows[i], rows[j] = rows[j], rows[i]
	}
	spec.Rows = rows
	return spec
}

// SQL asks the dialect to build the fragments, then renders them into text and parameters.
func (ins Insert[T]) SQL(d InsertDialect) (sql.Query, error) {
	spec := ins.Spec()
	if q, ok, err := tryInsert(d, spec); err != nil || ok {
		return q, err
	}
	built, err := d.BuildInsertQuery(spec)
	if err != nil {
		return sql.Query{}, err
	}
	return sql.Render(built, d, sql.RenderConfig{})
}

type setNode[T any] struct {
	prev *setNode[T]
	col  Column[T]
	val  any
}

type updateJoin struct {
	prev  *updateJoin
	kind  JoinKind
	table sql.Chunk
	on    sql.Chunk
}

// Update is an immutable update chain.
type Update[T any] struct {
	table Table[T]
	// setCols and setVals hold up to four assignments without a heap node.
	setCols   [4]Column[T]
	setVals   [4]any
	nSets     int
	sets      *setNode[T]
	from      sql.Chunk
	joins     *updateJoin
	where     sql.Chunk
	order     []sql.Chunk
	limit     int
	limitSet  bool
	returning []Column[T]
	withs     []WithSpec
}

// NewUpdate starts an update. Callers write pg.Update.
func NewUpdate[T any](table Table[T]) Update[T] {
	return Update[T]{table: table}
}

// Set appends one assignment. Calling it again on the same column writes that column once more and does not merge them.
// Zero values are written as given. nil binds NULL. An SQL fragment enters the statement as written.
func (u Update[T]) Set(col Column[T], value any) Update[T] {
	if u.sets == nil && u.nSets < len(u.setCols) {
		u.setCols[u.nSets] = col
		u.setVals[u.nSets] = nullValue(value)
		u.nSets++
		return u
	}
	if u.sets == nil && u.nSets > 0 {
		var head *setNode[T]
		for i := u.nSets - 1; i >= 0; i-- {
			head = &setNode[T]{prev: head, col: u.setCols[i], val: u.setVals[i]}
		}
		u.sets = head
		u.nSets = 0
	}
	u.sets = &setNode[T]{prev: u.sets, col: col, val: nullValue(value)}
	return u
}

// From sets an additional source. A later call replaces the previous one. nil means there is no from.
// The source may be another table, a derived table, or a CTE. To join more tables, write Join after From.
func (u Update[T]) From(table sql.Chunk) Update[T] {
	u.from = table
	return u
}

// InnerJoin appends an inner join.
func (u Update[T]) InnerJoin(table, on sql.Chunk) Update[T] {
	return u.addJoin(InnerJoin, table, on)
}

// LeftJoin appends a left join.
func (u Update[T]) LeftJoin(table, on sql.Chunk) Update[T] {
	return u.addJoin(LeftJoin, table, on)
}

// RightJoin appends a right join.
func (u Update[T]) RightJoin(table, on sql.Chunk) Update[T] {
	return u.addJoin(RightJoin, table, on)
}

// FullJoin appends a full join.
func (u Update[T]) FullJoin(table, on sql.Chunk) Update[T] {
	return u.addJoin(FullJoin, table, on)
}

func (u Update[T]) addJoin(kind JoinKind, table, on sql.Chunk) Update[T] {
	u.joins = &updateJoin{prev: u.joins, kind: kind, table: table, on: on}
	return u
}

// With adds CTEs in front of the statement. Repeated calls keep them in the order written.
func (u Update[T]) With(ctes ...WithSource) Update[T] {
	u.withs = appendWith(u.withs, ctes...)
	return u
}

// Where sets the condition. A later call replaces the previous one. nil means there is no where.
func (u Update[T]) Where(cond sql.Chunk) Update[T] {
	u.where = cond
	return u
}

// OrderBy sets the ordering. A later call replaces the previous one. Only single-table MySQL renders it.
func (u Update[T]) OrderBy(exprs ...sql.Chunk) Update[T] {
	u.order = append([]sql.Chunk(nil), exprs...)
	return u
}

// Limit sets the maximum number of rows to change. Limit(0) renders as limit 0. Only single-table MySQL renders it.
func (u Update[T]) Limit(n int) Update[T] {
	u.limit = n
	u.limitSet = true
	return u
}

// Returning sets RETURNING. A later call replaces the previous one.
func (u Update[T]) Returning(cols ...Column[T]) Update[T] {
	u.returning = append([]Column[T](nil), cols...)
	return u
}

// Spec expands this chain. Set is restored to call order.
func (u Update[T]) Spec() UpdateSpec {
	sets := assignments(u.sets)
	if u.sets == nil && u.nSets > 0 {
		sets = make([]Assignment, u.nSets)
		for i := 0; i < u.nSets; i++ {
			name := ""
			if u.setCols[i] != nil {
				name = u.setCols[i].ColName()
			}
			sets[i] = Assignment{Column: name, Value: u.setVals[i], Encoder: columnEncoder(u.setCols[i])}
		}
	}
	return UpdateSpec{
		Table:     u.table,
		Sets:      sets,
		From:      u.from,
		Joins:     updateJoins(u.joins),
		Where:     u.where,
		OrderBy:   append([]sql.Chunk(nil), u.order...),
		Limit:     u.limit,
		LimitSet:  u.limitSet,
		Returning: columnChunks(u.returning),
		With:      append([]WithSpec(nil), u.withs...),
	}
}

// SQL asks the dialect to build the fragments, then renders them into text and parameters.
func (u Update[T]) SQL(d UpdateDialect) (sql.Query, error) {
	spec := u.Spec()
	if q, ok, err := tryUpdate(d, spec); err != nil || ok {
		return q, err
	}
	built, err := d.BuildUpdateQuery(spec)
	if err != nil {
		return sql.Query{}, err
	}
	return sql.Render(built, d, sql.RenderConfig{})
}

// Delete is an immutable delete chain.
type Delete[T any] struct {
	table     Table[T]
	using     sql.Chunk
	joins     *updateJoin
	where     sql.Chunk
	order     []sql.Chunk
	limit     int
	limitSet  bool
	returning []Column[T]
	withs     []WithSpec
}

// NewDelete starts a delete. Callers write pg.Delete.
func NewDelete[T any](table Table[T]) Delete[T] {
	return Delete[T]{table: table}
}

// With adds CTEs in front of the statement. Repeated calls keep them in the order written.
func (d Delete[T]) With(ctes ...WithSource) Delete[T] {
	d.withs = appendWith(d.withs, ctes...)
	return d
}

// Using sets an additional source. A later call replaces the previous one. nil means there is no using.
func (d Delete[T]) Using(table sql.Chunk) Delete[T] {
	d.using = table
	return d
}

// InnerJoin appends an inner join.
func (d Delete[T]) InnerJoin(table, on sql.Chunk) Delete[T] {
	return d.addJoin(InnerJoin, table, on)
}

// LeftJoin appends a left join.
func (d Delete[T]) LeftJoin(table, on sql.Chunk) Delete[T] {
	return d.addJoin(LeftJoin, table, on)
}

// RightJoin appends a right join.
func (d Delete[T]) RightJoin(table, on sql.Chunk) Delete[T] {
	return d.addJoin(RightJoin, table, on)
}

// FullJoin appends a full join.
func (d Delete[T]) FullJoin(table, on sql.Chunk) Delete[T] {
	return d.addJoin(FullJoin, table, on)
}

func (d Delete[T]) addJoin(kind JoinKind, table, on sql.Chunk) Delete[T] {
	d.joins = &updateJoin{prev: d.joins, kind: kind, table: table, on: on}
	return d
}

// Where sets the condition. A later call replaces the previous one. If Where is not called, there is no where clause.
func (d Delete[T]) Where(cond sql.Chunk) Delete[T] {
	d.where = cond
	return d
}

// OrderBy sets the ordering. A later call replaces the previous one. Only single-table MySQL renders it.
func (d Delete[T]) OrderBy(exprs ...sql.Chunk) Delete[T] {
	d.order = append([]sql.Chunk(nil), exprs...)
	return d
}

// Limit sets the maximum number of rows to delete. Limit(0) renders as limit 0. Only single-table MySQL renders it.
func (d Delete[T]) Limit(n int) Delete[T] {
	d.limit = n
	d.limitSet = true
	return d
}

// Returning sets RETURNING. A later call replaces the previous one.
func (d Delete[T]) Returning(cols ...Column[T]) Delete[T] {
	d.returning = append([]Column[T](nil), cols...)
	return d
}

// Spec expands this chain.
func (d Delete[T]) Spec() DeleteSpec {
	return DeleteSpec{
		Table:     d.table,
		Using:     d.using,
		Joins:     updateJoins(d.joins),
		Where:     d.where,
		OrderBy:   append([]sql.Chunk(nil), d.order...),
		Limit:     d.limit,
		LimitSet:  d.limitSet,
		Returning: columnChunks(d.returning),
		With:      append([]WithSpec(nil), d.withs...),
	}
}

// SQL asks the dialect to build the fragments, then renders them into text and parameters.
func (d Delete[T]) SQL(dl DeleteDialect) (sql.Query, error) {
	built, err := dl.BuildDeleteQuery(d.Spec())
	if err != nil {
		return sql.Query{}, err
	}
	return sql.Render(built, dl, sql.RenderConfig{})
}

func updateJoins(head *updateJoin) []Join {
	var list []Join
	for n := head; n != nil; n = n.prev {
		list = append(list, Join{Kind: n.kind, Table: n.table, On: n.on})
	}
	for i, j := 0, len(list)-1; i < j; i, j = i+1, j-1 {
		list[i], list[j] = list[j], list[i]
	}
	return list
}

func assignments[T any](head *setNode[T]) []Assignment {
	var sets []Assignment
	for n := head; n != nil; n = n.prev {
		name := ""
		if n.col != nil {
			name = n.col.ColName()
		}
		sets = append(sets, Assignment{Column: name, Value: n.val, Encoder: columnEncoder(n.col)})
	}
	for i, j := 0, len(sets)-1; i < j; i, j = i+1, j-1 {
		sets[i], sets[j] = sets[j], sets[i]
	}
	return sets
}

func columnEncoder[T any](col Column[T]) sql.Encoder {
	if col == nil {
		return nil
	}
	return col.DriverEncoder()
}

func columnEncoders[T any](cols []Column[T]) []sql.Encoder {
	out := make([]sql.Encoder, len(cols))
	for i, col := range cols {
		out[i] = columnEncoder(col)
	}
	return out
}

func columnNames[T any](cols []Column[T]) []string {
	out := make([]string, len(cols))
	for i, col := range cols {
		out[i] = col.ColName()
	}
	return out
}

func columnChunks[T any](cols []Column[T]) []sql.Chunk {
	out := make([]sql.Chunk, len(cols))
	for i, col := range cols {
		out[i] = col
	}
	return out
}

// nullValue folds a nil pointer or a nil slice into a nil interface so the parameter binds as NULL.
// Zero values themselves stay: both 0 and "" are written into the SQL.
func nullValue(v any) any {
	switch v.(type) {
	case nil, string, bool, []byte,
		int, int8, int16, int32, int64,
		uint, uint8, uint16, uint32, uint64,
		float32, float64:
		return v
	}
	rv := reflect.ValueOf(v)
	switch rv.Kind() {
	case reflect.Pointer, reflect.Slice, reflect.Map:
		if rv.IsNil() {
			return nil
		}
	}
	return v
}
