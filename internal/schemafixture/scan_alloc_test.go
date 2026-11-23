package schemafixture

import "testing"

type noopRow struct{}

func (noopRow) Scan(...any) error { return nil }

func handScan(row interface{ Scan(...any) error }, u *User) error {
	return row.Scan(&u.ID, &u.Name, &u.Email, &u.CreatedAt)
}

func TestWholeRowScanAllocsMatchHandwritten(t *testing.T) {
	row := noopRow{}
	gen := testing.AllocsPerRun(1000, func() {
		var u User
		if err := scanUser(row, &u); err != nil {
			t.Fatal(err)
		}
	})
	hand := testing.AllocsPerRun(1000, func() {
		var u User
		if err := handScan(row, &u); err != nil {
			t.Fatal(err)
		}
	})
	if gen != hand {
		t.Fatalf("generated scan allocs/op = %v, handwritten = %v", gen, hand)
	}
}
