package util

import (
	"fmt"
	"testing"
)

func TestFormatSQL(t *testing.T) {
	got := FormatSQL("select id, name from users where id = 1")
	want := "SELECT id, name\nFROM users\nWHERE id = 1"
	if got != want {
		t.Errorf("FormatSQL =\n%q\nwant\n%q", got, want)
	}
	if FormatSQL("") != "" {
		t.Error("空输入应返回空")
	}
}

func ExampleFormatSQL() {
	fmt.Println(FormatSQL("select id, name from users where id = 1"))
	// Output:
	// SELECT id, name
	// FROM users
	// WHERE id = 1
}
