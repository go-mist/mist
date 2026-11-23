package migrate

import (
	"crypto/sha256"
	"encoding/binary"
)

// advisoryKey is derived from the schema and table name. Different migration tables do not block one another.
// The lock is pg_advisory_xact_lock and is released when the transaction commits or rolls back.
func advisoryKey(schema, table string) int64 {
	sum := sha256.Sum256([]byte("mist migrate\x00" + schema + "\x00" + table))
	return int64(binary.BigEndian.Uint64(sum[:8]))
}
