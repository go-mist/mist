package sqlite

import (
	"errors"

	"github.com/go-mist/mist/query"
	"github.com/go-mist/mist/sql"
)

type queryExpr interface {
	QuerySpec() query.SelectSpec
}

type derivedQuery interface {
	DerivedQuery() (string, query.SelectSpec)
}

// embeddedQuery is the expanded scalar subquery. The renderer wraps it in parentheses.
type embeddedQuery struct {
	body *sql.SQL
}

func (e embeddedQuery) GetSQL() *sql.SQL { return e.body }

// flattenCmp turns a comparison wrapper into its fragment list so ilike rewrite can see the operator.
func flattenCmp(chunks []sql.Chunk) ([]sql.Chunk, bool) {
	type comparer interface {
		Compare() (sql.Chunk, string, any)
	}
	changed := false
	for i, ch := range chunks {
		if ch == nil {
			continue
		}
		if _, ok := ch.(comparer); !ok {
			continue
		}
		if !changed {
			chunks = append([]sql.Chunk(nil), chunks...)
			changed = true
		}
		chunks[i] = ch.GetSQL()
	}
	return chunks, changed
}

func hasILike(chunks []sql.Chunk) bool {
	for _, ch := range chunks {
		frag, ok := ch.(sql.Fragment)
		if ok && (frag == " ilike " || frag == " not ilike ") {
			return true
		}
	}
	return false
}

// rewriteILike rewrites ilike into lower(left) like lower(right). SQLite has no ilike.
func rewriteILike(chunks []sql.Chunk) ([]sql.Chunk, bool) {
	if !hasILike(chunks) {
		return chunks, false
	}
	out := make([]sql.Chunk, 0, len(chunks))
	changed := false
	for i := 0; i < len(chunks); i++ {
		frag, ok := chunks[i].(sql.Fragment)
		if ok && (frag == " ilike " || frag == " not ilike ") && len(out) > 0 && i+1 < len(chunks) {
			left := out[len(out)-1]
			out = out[:len(out)-1]
			word := ") like lower("
			if frag == " not ilike " {
				word = ") not like lower("
			}
			out = append(out, sql.Raw("lower("), left, sql.Raw(word), chunks[i+1], sql.Raw(")"))
			i++
			changed = true
			continue
		}
		out = append(out, chunks[i])
	}
	if !changed {
		return chunks, false
	}
	return out, true
}

func unaliasedSelect(ch sql.Chunk) error {
	if _, ok := ch.(queryExpr); ok {
		if _, derived := ch.(derivedQuery); !derived {
			return errors.New("sqlite: a subquery in from or join needs an alias; use As")
		}
	}
	return nil
}

func (d Dialect) expandSQL(s *sql.SQL) (*sql.SQL, error) {
	if s == nil {
		return nil, nil
	}
	if flat, ok := flattenCmp(s.QueryChunks); ok {
		s = &sql.SQL{QueryChunks: flat}
	}
	if next, ok := rewriteILike(s.QueryChunks); ok {
		s = &sql.SQL{QueryChunks: next}
	}
	chunks := s.QueryChunks
	changed := false
	for i, ch := range s.QueryChunks {
		next, replaced, err := d.expandChunk(ch)
		if err != nil {
			return nil, err
		}
		if !replaced {
			continue
		}
		if !changed {
			chunks = append([]sql.Chunk(nil), s.QueryChunks...)
			changed = true
		}
		chunks[i] = next
	}
	if !changed {
		return s, nil
	}
	return &sql.SQL{QueryChunks: chunks}, nil
}

func (d Dialect) expandChunk(ch sql.Chunk) (sql.Chunk, bool, error) {
	if ch == nil {
		return nil, false, nil
	}
	if src, ok := ch.(derivedQuery); ok {
		alias, spec := src.DerivedQuery()
		if alias == "" {
			return nil, false, errors.New("sqlite: subquery is missing an alias")
		}
		body, err := d.BuildSelectQuery(spec)
		if err != nil {
			return nil, false, err
		}
		return sql.NewSubquery(body, alias), true, nil
	}
	if src, ok := ch.(queryExpr); ok {
		body, err := d.BuildSelectQuery(src.QuerySpec())
		if err != nil {
			return nil, false, err
		}
		return embeddedQuery{body: body}, true, nil
	}
	if rows, ok := ch.(*sql.ValueRows); ok {
		return d.expandValueRows(rows)
	}
	nested, ok := ch.(*sql.SQL)
	if !ok {
		return ch, false, nil
	}
	next, err := d.expandSQL(nested)
	if err != nil {
		return nil, false, err
	}
	if next == nested {
		return ch, false, nil
	}
	return next, true, nil
}

func (d Dialect) expandValueRows(rows *sql.ValueRows) (sql.Chunk, bool, error) {
	if rows == nil {
		return rows, false, nil
	}
	cells := rows.Cells
	changed := false
	for i, cell := range rows.Cells {
		next, replaced, err := d.expandValue(cell)
		if err != nil {
			return nil, false, err
		}
		if !replaced {
			continue
		}
		if !changed {
			cells = append([]any(nil), rows.Cells...)
			changed = true
		}
		cells[i] = next
	}
	if !changed {
		return rows, false, nil
	}
	return &sql.ValueRows{Cells: cells, Encoders: rows.Encoders}, true, nil
}

func (d Dialect) expandValue(cell any) (any, bool, error) {
	if src, ok := cell.(queryExpr); ok {
		body, err := d.BuildSelectQuery(src.QuerySpec())
		if err != nil {
			return nil, false, err
		}
		return sql.S(sql.Raw("("), body, sql.Raw(")")), true, nil
	}
	if _, ok := cell.(derivedQuery); ok {
		return nil, false, errors.New("sqlite: values needs a scalar subquery, not an aliased derived table")
	}
	nested, ok := cell.(*sql.SQL)
	if !ok {
		return cell, false, nil
	}
	next, err := d.expandSQL(nested)
	if err != nil {
		return nil, false, err
	}
	if next == nested {
		return cell, false, nil
	}
	return next, true, nil
}
