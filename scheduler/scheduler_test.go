package scheduler

import (
	"context"
	"fmt"
	"sync/atomic"
	"testing"
	"time"
)

func TestParseSpecDaily(t *testing.T) {
	sp, err := parseSpec("daily 09:30", time.UTC)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if sp.kind != kindDaily || sp.hour != 9 || sp.minute != 30 {
		t.Fatalf("sp=%+v", sp)
	}
}

func TestParseSpecWeekly(t *testing.T) {
	sp, err := parseSpec("weekly Mon 08:00", time.UTC)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if sp.kind != kindWeekly || sp.weekday != 1 || sp.hour != 8 {
		t.Fatalf("sp=%+v", sp)
	}
	sp, _ = parseSpec("weekly * 08:00", time.UTC)
	if sp.weekday != -1 {
		t.Fatal("weekday * should be -1")
	}
}

func TestParseSpecEvery(t *testing.T) {
	sp, err := parseSpec("every 6h", time.UTC)
	if err != nil || sp.kind != kindEvery || sp.every != 6*time.Hour {
		t.Fatalf("sp=%+v err=%v", sp, err)
	}
}

func TestParseSpecInvalid(t *testing.T) {
	cases := []string{"", "hourly 09:00", "daily 25:00", "daily 9:99", "weekly Foo 08:00", "every notadur"}
	for _, c := range cases {
		if _, err := parseSpec(c, time.UTC); err == nil {
			t.Fatalf("expected error for %q", c)
		}
	}
}

func TestNextFromEvery(t *testing.T) {
	now := time.Now()
	sp := spec{kind: kindEvery, every: time.Hour, loc: time.UTC}
	if next := sp.nextFrom(now); next.Sub(now) != time.Hour {
		t.Fatalf("next=%v", next)
	}
}

func TestAddJobInvalid(t *testing.T) {
	s := New()
	if err := s.AddJob("x", "bogus", func(ctx context.Context) error { return nil }); err == nil {
		t.Fatal("expected error for invalid spec")
	}
}

func TestScheduleExecution(t *testing.T) {
	var count int64
	s := New(WithInterval(10 * time.Millisecond))
	if err := s.AddJob("t", "every 20ms", func(ctx context.Context) error {
		atomic.AddInt64(&count, 1)
		return nil
	}); err != nil {
		t.Fatalf("addjob: %v", err)
	}
	s.Start()
	time.Sleep(120 * time.Millisecond)
	s.Stop()
	if atomic.LoadInt64(&count) < 2 {
		t.Fatalf("job executed %d times, want >=2", count)
	}
}

func TestOptions(t *testing.T) {
	s := New(WithConcurrency(4), WithInterval(time.Second), WithLocation(time.UTC))
	if cap(s.sem) != 4 {
		t.Fatal("concurrency option wrong")
	}
	if s.interval != time.Second {
		t.Fatal("interval option wrong")
	}
	if s.loc != time.UTC {
		t.Fatal("location option wrong")
	}
}

func ExampleScheduler_AddJob() {
	s := New(WithConcurrency(2))
	_ = s.AddJob("backup", "daily 02:00", func(ctx context.Context) error {
		return nil
	})
	fmt.Println(s != nil)
	// Output: true
}
