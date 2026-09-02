package response

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func decode(t *testing.T, w *httptest.ResponseRecorder) Response {
	t.Helper()
	var r Response
	if err := json.Unmarshal(w.Body.Bytes(), &r); err != nil {
		t.Fatalf("decode: %v body=%s", err, w.Body.String())
	}
	return r
}

func newCtx() (*gin.Context, *httptest.ResponseRecorder) {
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	return c, w
}

func TestSuccess(t *testing.T) {
	c, w := newCtx()
	Success(c, gin.H{"k": "v"})
	if w.Code != http.StatusOK {
		t.Fatalf("code=%d", w.Code)
	}
	r := decode(t, w)
	if r.Code != 0 || r.Message != "success" {
		t.Fatalf("resp=%+v", r)
	}
}

func TestSuccessWithMessage(t *testing.T) {
	c, w := newCtx()
	SuccessWithMessage(c, "hi", nil)
	r := decode(t, w)
	if r.Code != 0 || r.Message != "hi" {
		t.Fatalf("resp=%+v", r)
	}
}

func TestSuccessPage(t *testing.T) {
	c, w := newCtx()
	SuccessPage(c, 100, 1, 10, []int{1, 2})
	r := decode(t, w)
	if r.Code != 0 {
		t.Fatalf("code=%d", r.Code)
	}
	if r.Data == nil {
		t.Fatal("data is nil")
	}
}

func TestError(t *testing.T) {
	c, w := newCtx()
	Error(c, 12345, "boom")
	if w.Code != http.StatusOK {
		t.Fatalf("code=%d", w.Code)
	}
	r := decode(t, w)
	if r.Code != 12345 || r.Message != "boom" {
		t.Fatalf("resp=%+v", r)
	}
}

func TestErrorWithStatus(t *testing.T) {
	c, w := newCtx()
	ErrorWithStatus(c, http.StatusBadRequest, 10001, "bad")
	if w.Code != http.StatusBadRequest {
		t.Fatalf("code=%d", w.Code)
	}
	r := decode(t, w)
	if r.Code != 10001 || r.Message != "bad" {
		t.Fatalf("resp=%+v", r)
	}
}

func TestErrorHelpers(t *testing.T) {
	// 需传入 message 的构造函数
	withMsg := func(fn func(*gin.Context, string)) func(*gin.Context) {
		return func(c *gin.Context) { fn(c, "test message") }
	}
	cases := []struct {
		name   string
		call   func(c *gin.Context)
		code   int
		status int
	}{
		{"BadRequest", withMsg(BadRequest), 10001, http.StatusBadRequest},
		{"Unauthorized", withMsg(Unauthorized), 10002, http.StatusUnauthorized},
		{"Forbidden", withMsg(Forbidden), 10003, http.StatusForbidden},
		{"NotFound", withMsg(NotFound), 10004, http.StatusNotFound},
		{"InternalServerError", withMsg(InternalServerError), 10005, http.StatusInternalServerError},
		{"TooManyRequests", withMsg(TooManyRequests), 10008, http.StatusTooManyRequests},
		{"ParamError", withMsg(ParamError), 10001, http.StatusBadRequest},
		{"PermissionDenied", PermissionDenied, 10003, http.StatusOK},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c, w := newCtx()
			tc.call(c)
			if w.Code != tc.status {
				t.Fatalf("status=%d want %d", w.Code, tc.status)
			}
			r := decode(t, w)
			if r.Code != tc.code {
				t.Fatalf("code=%d want %d", r.Code, tc.code)
			}
		})
	}
}
