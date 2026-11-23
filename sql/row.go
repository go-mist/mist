package sql

// Row is one row of a query result. Row and Rows from database/sql and pgx all satisfy this interface.
type Row interface {
	Scan(dest ...any) error
}
