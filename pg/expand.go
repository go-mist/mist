package pg

import (
	"errors"

	"github.com/xbmlz/mist/query"
	"github.com/xbmlz/mist/sql"
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

func unaliasedSelect(ch sql.Chunk) error {
	if _, ok := ch.(queryExpr); ok {
		if _, derived := ch.(derivedQuery); !derived {
			return errors.New("pg: a subquery in from or join needs an alias; use As")
		}
	}
	return nil
}

func (d Dialect) expandSQL(s *sql.SQL) (*sql.SQL, error) {
	if s == nil {
		return nil, nil
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
			return nil, false, errors.New("pg: subquery is missing an alias")
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
		return nil, false, errors.New("pg: values needs a scalar subquery, not an aliased derived table")
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
