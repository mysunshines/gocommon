package prometheus

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestNew(t *testing.T) {
	c := New("http://localhost:9090", 0)
	if c == nil {
		t.Fatal("New returned nil")
	}
	c2 := New("http://x", 5*time.Second)
	if c2 == nil {
		t.Fatal("New returned nil")
	}
}

func TestParseSample(t *testing.T) {
	ts, v, ok := parseSample([2]interface{}{float64(1600000000), "1.5"})
	if !ok || v != 1.5 {
		t.Fatalf("parseSample failed: %v %v %v", ts, v, ok)
	}
	if _, _, ok := parseSample([2]interface{}{"bad", "1.5"}); ok {
		t.Fatal("expected false for non-float timestamp")
	}
	if _, _, ok := parseSample([2]interface{}{float64(1), 5}); ok {
		t.Fatal("expected false for non-string value")
	}
}

func TestTruncate(t *testing.T) {
	if truncate("abc", 10) != "abc" {
		t.Fatal("should not truncate short string")
	}
	if truncate("abcdef", 3) != "abc..." {
		t.Fatalf("truncate wrong: %q", truncate("abcdef", 3))
	}
}

func TestQuery(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/query" {
			t.Errorf("unexpected path %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"status": "success",
			"data": map[string]interface{}{
				"resultType": "vector",
				"result": []interface{}{
					map[string]interface{}{
						"metric": map[string]string{"__name__": "up"},
						"value":  [2]interface{}{float64(1600000000), "1"},
					},
				},
			},
		})
	}))
	defer srv.Close()

	c := New(srv.URL, time.Second)
	res, err := c.Query(context.Background(), "up", time.Time{})
	if err != nil {
		t.Fatalf("Query err: %v", err)
	}
	if res.ResultType != "vector" || len(res.Series) != 1 {
		t.Fatalf("bad result: %+v", res)
	}
	if res.Series[0].Value == nil || res.Series[0].Value.Value != 1 {
		t.Fatalf("bad value: %+v", res.Series[0].Value)
	}
}

func TestQueryRange(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"status": "success",
			"data": map[string]interface{}{
				"resultType": "matrix",
				"result": []interface{}{
					map[string]interface{}{
						"metric": map[string]string{"__name__": "rate"},
						"values": [][2]interface{}{
							{float64(1600000000), "1"},
							{float64(1600000060), "2"},
						},
					},
				},
			},
		})
	}))
	defer srv.Close()

	c := New(srv.URL, time.Second)
	start := time.Unix(1600000000, 0)
	end := time.Unix(1600000060, 0)
	res, err := c.QueryRange(context.Background(), "rate", start, end, time.Minute)
	if err != nil {
		t.Fatalf("QueryRange err: %v", err)
	}
	if res.ResultType != "matrix" || len(res.Series) != 1 {
		t.Fatalf("bad result: %+v", res)
	}
	if len(res.Series[0].Samples) != 2 {
		t.Fatalf("expected 2 samples, got %d", len(res.Series[0].Samples))
	}
}

func TestQueryNonSuccess(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"status": "error",
			"error":  "bad query",
		})
	}))
	defer srv.Close()

	c := New(srv.URL, time.Second)
	if _, err := c.Query(context.Background(), "bad", time.Time{}); err == nil {
		t.Fatal("expected error for non-success status")
	}
}

func TestQueryHTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("boom"))
	}))
	defer srv.Close()

	c := New(srv.URL, time.Second)
	if _, err := c.Query(context.Background(), "up", time.Time{}); err == nil {
		t.Fatal("expected error for non-200 status")
	}
}

func ExampleNew() {
	c := New("http://prometheus:9090", 30*time.Second)
	_, _ = c.Query(context.Background(), "up", time.Time{})
}
