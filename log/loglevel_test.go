package log

import "testing"

func TestSetLevelValid(t *testing.T) {
	if err := SetLevel("debug"); err != nil {
		t.Fatalf("SetLevel(debug) unexpected error: %v", err)
	}
	if err := SetLevel("warn"); err != nil {
		t.Fatalf("SetLevel(warn) unexpected error: %v", err)
	}
}

func TestSetLevelInvalid(t *testing.T) {
	if err := SetLevel("not-a-level"); err == nil {
		t.Fatal("SetLevel(invalid) should return error")
	}
}
