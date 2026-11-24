package sqlite_test

import (
	"context"
	"strings"
	"testing"

	"github.com/go-mist/mist/dialect/sqlite"
)

func TestOpenNamesTheFailure(t *testing.T) {
	ctx := context.Background()
	_, err := sqlite.Open(ctx, "")
	if err == nil || err.Error() != "sqlite: missing connection string" {
		t.Fatal(err)
	}
	_, err = sqlite.Open(ctx, t.TempDir()+"/missing/app.db")
	if err == nil || !strings.Contains(err.Error(), "sqlite: connect:") {
		t.Fatal(err)
	}
}
