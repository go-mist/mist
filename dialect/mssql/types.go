package mssql

import "fmt"

// ColumnType is the SQL Server column type for a portable Go field.
// An integer primary key is an identity column. A string with length 0 is nvarchar(max).
func ColumnType(goType string, pk bool, length int) (string, error) {
	switch goType {
	case "int64":
		if pk {
			return "bigint identity(1,1)", nil
		}
		return "bigint", nil
	case "int", "int32":
		if pk {
			return "int identity(1,1)", nil
		}
		return "int", nil
	case "string":
		if length <= 0 {
			return "nvarchar(max)", nil
		}
		if length > 4000 {
			return "", fmt.Errorf("mssql: string length %d is above nvarchar(4000)", length)
		}
		return fmt.Sprintf("nvarchar(%d)", length), nil
	case "bool":
		return "bit", nil
	case "time.Time":
		return "datetimeoffset", nil
	case "[]byte":
		return "varbinary(max)", nil
	default:
		return "", fmt.Errorf("mssql: %s has no column type", goType)
	}
}
