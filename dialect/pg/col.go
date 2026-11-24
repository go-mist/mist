package pg

import "github.com/go-mist/mist"

// Row is one row of a query result.
type Row = mist.Row

// TableMeta is mist.TableMeta. Existing schemas can keep writing pg.TableMeta.
type TableMeta = mist.TableMeta

// Index is an index on a table.
type Index = mist.Index

// Check is a CHECK on a table.
type Check = mist.Check

// ColumnSpec is the column constraint copied from a field tag.
type ColumnSpec = mist.ColumnSpec

// TableBase is the generated table descriptor.
type TableBase = mist.TableBase

// Col is the generated column descriptor.
type Col[T, V any] = mist.Col[T, V]
