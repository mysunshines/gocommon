package constants

import (
	"fmt"
	"testing"
)

func TestErrorCodes(t *testing.T) {
	checks := []struct {
		name string
		got  int
		want int
	}{
		{"ErrCodeSuccess", ErrCodeSuccess, 0},
		{"ErrCodeBadRequest", ErrCodeBadRequest, 10001},
		{"ErrCodeUnauthorized", ErrCodeUnauthorized, 10002},
		{"ErrCodeForbidden", ErrCodeForbidden, 10003},
		{"ErrCodeNotFound", ErrCodeNotFound, 10004},
		{"ErrCodeInternal", ErrCodeInternal, 10005},
		{"ErrCodeServiceUnavailable", ErrCodeServiceUnavailable, 10006},
		{"ErrCodeTimeout", ErrCodeTimeout, 10007},
		{"ErrCodeRateLimited", ErrCodeRateLimited, 10008},
		{"ErrCodeUserExists", ErrCodeUserExists, 20001},
		{"ErrCodeTokenInvalid", ErrCodeTokenInvalid, 20002},
		{"ErrCodePasswordIncorrect", ErrCodePasswordIncorrect, 20003},
		{"ErrCodeUserInBlacklist", ErrCodeUserInBlacklist, 20004},
		{"ErrCodeUserNotFound", ErrCodeUserNotFound, 20005},
		{"ErrCodeTokenExpired", ErrCodeTokenExpired, 20006},
		{"ErrCodeArticleNotFound", ErrCodeArticleNotFound, 30001},
		{"ErrCodeCommentNotFound", ErrCodeCommentNotFound, 40001},
		{"ErrCodeCommentDisabled", ErrCodeCommentDisabled, 40003},
		{"ErrCodeCommentBlacklist", ErrCodeCommentBlacklist, 40004},
	}
	for _, c := range checks {
		if c.got != c.want {
			t.Errorf("%s = %d, want %d", c.name, c.got, c.want)
		}
	}
}

func TestRoles(t *testing.T) {
	if RoleNormal != 1 || RoleAdmin != 2 {
		t.Fatal("role constants wrong")
	}
}

func TestTimeFormatConstants(t *testing.T) {
	if DateTimeFormat == "" || DateFormat == "" || DateTimeISO8601 == "" || DateTimeWithTZ == "" {
		t.Fatal("time format constants empty")
	}
}

func TestServiceNameConstants(t *testing.T) {
	if ServiceNameUser != "user-service" || ServiceNameGateway != "gateway" {
		t.Fatal("service name constants wrong")
	}
}

func ExampleErrCodeBadRequest() {
	fmt.Println(ErrCodeBadRequest)
	// Output: 10001
}
