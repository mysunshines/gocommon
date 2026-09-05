package util

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

func TestJSONFormat(t *testing.T) {
	got, err := JSONFormat(`{"name":"张三","age":18}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(got, "\n") || !strings.Contains(got, `"name": "张三"`) {
		t.Errorf("JSONFormat 未正确美化:\n%s", got)
	}
	if _, err := JSONFormat("not json"); err == nil {
		t.Error("非法 JSON 应返回错误")
	}
}

func TestJSONToYAML(t *testing.T) {
	got, err := JSONToYAML(`{"a":1}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "a: 1\n" {
		t.Errorf("JSONToYAML = %q", got)
	}
}

func TestYAMLToJSON(t *testing.T) {
	got, err := YAMLToJSON("a: 1\n")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := "{\n  \"a\": 1\n}"
	if got != want {
		t.Errorf("YAMLToJSON = %q, want %q", got, want)
	}
}

func TestNormalizeForJSON(t *testing.T) {
	in := map[any]any{"k": []any{1, map[any]any{"x": 2}}}
	got := NormalizeForJSON(in)
	out, err := json.Marshal(got)
	if err != nil {
		t.Fatalf("marshal err: %v", err)
	}
	if string(out) != `{"k":[1,{"x":2}]}` {
		t.Errorf("NormalizeForJSON = %s", out)
	}
}

func TestToPascalCase(t *testing.T) {
	cases := map[string]string{
		"hello_world": "HelloWorld",
		"hello-world": "HelloWorld",
		"hello world": "HelloWorld",
		"helloWorld":  "HelloWorld",
	}
	for in, want := range cases {
		if got := ToPascalCase(in); got != want {
			t.Errorf("ToPascalCase(%q) = %q, want %q", in, got, want)
		}
	}
}

func ExampleJSONFormat() {
	s, _ := JSONFormat(`{"name":"张三","age":18}`)
	fmt.Print(s)
	// Output:
	// {
	//   "age": 18,
	//   "name": "张三"
	// }
}

func ExampleJSONToYAML() {
	s, _ := JSONToYAML(`{"a":1}`)
	fmt.Print(s)
	// Output: a: 1
}

func ExampleYAMLToJSON() {
	s, _ := YAMLToJSON("a: 1\n")
	fmt.Print(s)
	// Output:
	// {
	//   "a": 1
	// }
}

func ExampleNormalizeForJSON() {
	in := map[any]any{"k": 1}
	out := NormalizeForJSON(in)
	fmt.Printf("%T\n", out)
	// Output: map[string]interface {}
}

func ExampleToPascalCase() {
	fmt.Println(ToPascalCase("hello_world"))
	fmt.Println(ToPascalCase("hello-world"))
	// Output:
	// HelloWorld
	// HelloWorld
}
