package pg_test

import (
	"context"
	"strings"
	"testing"

	"github.com/go-mist/mist/dialect/pg"
)

func TestOpenNamesTheFailure(t *testing.T) {
	ctx := context.Background()
	_, err := pg.Open(ctx, "")
	if err == nil || err.Error() != "pg: missing connection string" {
		t.Fatal(err)
	}
	_, err = pg.Open(ctx, "://")
	if err == nil || !strings.Contains(err.Error(), "pg: connection string:") {
		t.Fatal(err)
	}
}
