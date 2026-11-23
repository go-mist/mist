package migrate

import "fmt"

const (
	defaultSchema = "drizzle"
	defaultTable  = "__drizzle_migrations"
)

// CreateTableSQL is the v1 migration table. applied_at uses the database now(), and the caller does not write that value.
func CreateTableSQL(schema, table string) string {
	return fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s.%s (
				id SERIAL PRIMARY KEY,
				hash text NOT NULL,
				created_at bigint,
				name text,
				applied_at timestamp with time zone DEFAULT now()
			)`, quoteIdent(schema), quoteIdent(table))
}

func quoteIdent(name string) string {
	return `"` + name + `"`
}
