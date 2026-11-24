package mysql_test

import (
	"context"
	"testing"

	"github.com/go-mist/mist/dialect/mysql"
)

func TestOpenNamesTheFailure(t *testing.T) {
	_, err := mysql.Open(context.Background(), "")
	if err == nil || err.Error() != "mysql: missing connection string" {
		t.Fatal(err)
	}
}
