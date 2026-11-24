package dm

import "fmt"

// ColumnType is the Dameng column type for a portable Go field.
// An integer primary key is an identity column. A string with length 0 is CLOB.
func ColumnType(goType string, pk bool, length int) (string, error) {
	switch goType {
	case "int64":
		if pk {
			return "BIGINT IDENTITY(1,1)", nil
		}
		return "BIGINT", nil
	case "int", "int32":
		if pk {
			return "INT IDENTITY(1,1)", nil
		}
		return "INT", nil
	case "string":
		if length <= 0 {
			return "CLOB", nil
		}
		if length > 32767 {
			return "", fmt.Errorf("dm: string length %d is above VARCHAR(32767)", length)
		}
		return fmt.Sprintf("VARCHAR(%d)", length), nil
	case "bool":
		return "BIT", nil
	case "time.Time":
		return "TIMESTAMP", nil
	case "[]byte":
		return "BLOB", nil
	default:
		return "", fmt.Errorf("dm: %s has no column type", goType)
	}
}
