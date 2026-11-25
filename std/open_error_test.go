package std_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/go-mist/mist/std"
)

func TestOpenNamesTheFailure(t *testing.T) {
	ctx := context.Background()
	_, err := std.Open(ctx, "", "postgres://localhost/mist")
	if err == nil || err.Error() != "std: missing driver name" {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	_, err = std.Open(ctx, "pgx", "postgres://127.0.0.1:1/mist?sslmode=disable")
	if err == nil || !strings.Contains(err.Error(), "std: connect:") {
		t.Fatal(err)
	}
}
