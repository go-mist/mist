package mysql

// MySQL column type names. There are no arrays, no timestamptz, and no uuid type.
// Boolean is tinyint(1). This version does not coerce scan results into bool.
const (
	TypeBigInt    = "bigint"
	TypeVarChar   = "varchar"
	TypeChar      = "char"
	TypeDateTime  = "datetime"
	TypeTimestamp = "timestamp"
	TypeJSON      = "json"
	TypeDecimal   = "decimal"
	TypeBlob      = "blob"
	TypeTinyInt   = "tinyint"
)
