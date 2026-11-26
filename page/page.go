// Package page reads one window of a select.
//
// It does not parse SQL. Fetch runs the count from Select.RowCount, then the same select with limit and offset, unless Request.NoCount is set.
// FetchAfter keeps reading after the last value of one ordered column and does not count.
// The dialect still renders the statements.
// Request is the input. An HTTP handler can decode query or JSON fields into it.
// Result is filled by Fetch and FetchAfter.
package page

import (
	"context"
	"errors"
	"math"

	"github.com/go-mist/mist/op"
	"github.com/go-mist/mist/query"
	"github.com/go-mist/mist/sql"
)

// Request is the page input. Current starts at 1.
// A zero Current or Size is the unset value: Fetch uses 1 and 10.
// The tags are the field names an HTTP decoder binds. This package does not read the request.
type Request struct {
	Current int  `json:"current" form:"current"`
	Size    int  `json:"size" form:"size"`
	NoCount bool `json:"noCount" form:"noCount"`
}

// Cursor is the position after the last row already returned.
// Value nil reads the first window. A later call passes that column's value from the last row.
// Desc reads smaller values. The default reads greater values.
// Size 0 means 10. The column should be unique, because equal values are skipped or repeated.
type Cursor struct {
	Value any
	Size  int
	Desc  bool
}

// Result is one page. Current starts at 1.
// Pages is the number of pages for Total and Size. Records is empty when this page has no rows.
type Result[T any] struct {
	Records []T   `json:"records"`
	Total   int64 `json:"total"`
	Size    int   `json:"size"`
	Current int   `json:"current"`
	Pages   int64 `json:"pages"`
}

// Fetch returns one page of q.
// Order, limit, offset, and top already written on q are left out of the count.
// Limit and offset written on q are replaced by this page.
// NoCount skips the count. Total and Pages stay 0, and the window query still runs.
// SQL Server and Oracle still require OrderBy on q, because their offset syntax does.
func Fetch[T any](ctx context.Context, db query.Executor, q query.Select[T], req Request) (Result[T], error) {
	current, size, err := normalize(req)
	if err != nil {
		return Result[T]{}, err
	}
	if current > 1 && size > math.MaxInt/(current-1) {
		return Result[T]{}, errors.New("page: offset overflows")
	}
	offset := (current - 1) * size
	if req.NoCount {
		return window(ctx, db, q.Limit(size).Offset(offset), size, current)
	}
	total, err := q.RowCount().One(ctx, db)
	if err != nil {
		return Result[T]{}, err
	}
	out := Result[T]{
		Records: []T{},
		Total:   total,
		Size:    size,
		Current: current,
		Pages:   pageCount(total, size),
	}
	if total == 0 || int64(offset) >= total {
		return out, nil
	}
	items, err := q.Limit(size).Offset(offset).All(ctx, db)
	if items != nil {
		out.Records = items
	}
	return out, err
}

// FetchAfter returns the next window after cur on col.
// It replaces OrderBy with that column and does not run count(*). Total, Current, and Pages stay 0.
// A query that already has Offset or Top is refused.
func FetchAfter[T any](ctx context.Context, db query.Executor, q query.Select[T], col query.Column[T], cur Cursor) (Result[T], error) {
	if col == nil {
		return Result[T]{}, errors.New("page: cursor column is missing")
	}
	size := cur.Size
	if size == 0 {
		size = 10
	}
	if size < 1 {
		return Result[T]{}, errors.New("page: size must be at least 1")
	}
	spec := q.Spec()
	if spec.OffsetSet {
		return Result[T]{}, errors.New("page: cursor does not use offset")
	}
	if spec.TopSet {
		return Result[T]{}, errors.New("page: cursor does not use top")
	}
	order := op.Asc(col)
	cmp := op.Gt(col, cur.Value)
	if cur.Desc {
		order = op.Desc(col)
		cmp = op.Lt(col, cur.Value)
	}
	next := q.OrderBy(order).Limit(size)
	if cur.Value != nil {
		var cond sql.Chunk = cmp
		if spec.Where != nil {
			cond = op.And(spec.Where, cmp)
		}
		next = next.Where(cond)
	}
	return window(ctx, db, next, size, 0)
}

func window[T any](ctx context.Context, db query.Executor, q query.Select[T], size, current int) (Result[T], error) {
	items, err := q.All(ctx, db)
	out := Result[T]{
		Records: []T{},
		Size:    size,
		Current: current,
	}
	if items != nil {
		out.Records = items
	}
	return out, err
}

func normalize(req Request) (current, size int, err error) {
	current = req.Current
	if current == 0 {
		current = 1
	}
	size = req.Size
	if size == 0 {
		size = 10
	}
	if current < 1 {
		return 0, 0, errors.New("page: current starts at 1")
	}
	if size < 1 {
		return 0, 0, errors.New("page: size must be at least 1")
	}
	return current, size, nil
}

func pageCount(total int64, size int) int64 {
	if total <= 0 || size <= 0 {
		return 0
	}
	s := int64(size)
	return (total + s - 1) / s
}
