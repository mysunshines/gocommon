package retry

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"
)

func TestDoSuccessFirstTry(t *testing.T) {
	calls := 0
	err := Do(context.Background(), func() error {
		calls++
		return nil
	}, Options{Attempts: 3, Delay: time.Millisecond})
	if err != nil || calls != 1 {
		t.Fatalf("err=%v calls=%d", err, calls)
	}
}

func TestDoRetryThenSuccess(t *testing.T) {
	calls := 0
	err := Do(context.Background(), func() error {
		calls++
		if calls < 3 {
			return errors.New("transient")
		}
		return nil
	}, Options{Attempts: 5, Delay: time.Millisecond})
	if err != nil || calls != 3 {
		t.Fatalf("err=%v calls=%d", err, calls)
	}
}

func TestDoExhausted(t *testing.T) {
	calls := 0
	err := Do(context.Background(), func() error {
		calls++
		return errors.New("always")
	}, Options{Attempts: 2, Delay: time.Millisecond})
	if err == nil || calls != 2 {
		t.Fatalf("err=%v calls=%d", err, calls)
	}
}

func TestDoPermanent(t *testing.T) {
	calls := 0
	perr := errors.New("fatal")
	err := Do(context.Background(), func() error {
		calls++
		return Permanent(perr)
	}, Options{Attempts: 5, Delay: time.Millisecond})
	if !errors.Is(err, perr) || calls != 1 {
		t.Fatalf("err=%v calls=%d", err, calls)
	}
	if !IsPermanent(err) {
		t.Fatal("expected permanent error")
	}
}

func TestDoShouldRetryFalse(t *testing.T) {
	calls := 0
	err := Do(context.Background(), func() error {
		calls++
		return errors.New("stop")
	}, Options{
		Attempts:    5,
		Delay:       time.Millisecond,
		ShouldRetry: func(e error) bool { return false },
	})
	if err == nil || calls != 1 {
		t.Fatalf("err=%v calls=%d", err, calls)
	}
}

func TestDoContextCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := Do(ctx, func() error {
		return errors.New("should not run")
	}, Options{Attempts: 3, Delay: time.Millisecond})
	if err == nil {
		t.Fatal("expected context error")
	}
}

func TestDoDefaultAttempts(t *testing.T) {
	calls := 0
	_ = Do(context.Background(), func() error {
		calls++
		return errors.New("x")
	}, Options{Delay: time.Millisecond})
	if calls != 3 {
		t.Fatalf("default attempts should be 3, got %d", calls)
	}
}

func TestPermanentNil(t *testing.T) {
	if Permanent(nil) != nil {
		t.Fatal("Permanent(nil) should be nil")
	}
	if IsPermanent(nil) {
		t.Fatal("IsPermanent(nil) should be false")
	}
}

func ExampleDo() {
	err := Do(context.Background(), func() error {
		return nil
	}, Options{Attempts: 1})
	fmt.Println(err)
	// Output: <nil>
}
