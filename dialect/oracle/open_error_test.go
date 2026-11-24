package oracle_test

import (
	"context"
	"testing"

	"github.com/go-mist/mist/dialect/oracle"
)

func TestOpenNamesTheFailure(t *testing.T) {
	_, err := oracle.Open(context.Background(), "")
	if err == nil || err.Error() != "oracle: missing connection string" {
		t.Fatal(err)
	}
}
