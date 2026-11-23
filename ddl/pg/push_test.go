package pg

import "testing"

func TestConfirm(t *testing.T) {
	if !Confirm("y") || !Confirm(" YES ") || Confirm("") || Confirm("n") || Confirm("yess") {
		t.Fatal()
	}
}
