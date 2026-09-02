package pool

import (
	"context"
	"fmt"
	"testing"
	"time"
)

func TestParallel(t *testing.T) {
	p := New(WithMaxWorkers(4))
	results := p.Parallel(context.Background(),
		func(ctx context.Context) (interface{}, error) { return 1, nil },
		func(ctx context.Context) (interface{}, error) { return 2, nil },
		func(ctx context.Context) (interface{}, error) { return nil, fmt.Errorf("e") },
	)
	if len(results) != 3 {
		t.Fatalf("len=%d", len(results))
	}
	if results[0].Value != 1 || results[1].Value != 2 || results[2].Err == nil {
		t.Fatal("unexpected results")
	}
	if results[0].Index != 0 || results[2].Index != 2 {
		t.Fatal("index mismatch")
	}
}

func TestSerial(t *testing.T) {
	p := New()
	results := p.Serial(context.Background(),
		func(ctx context.Context) (interface{}, error) { return "a", nil },
		func(ctx context.Context) (interface{}, error) { return nil, fmt.Errorf("x") },
	)
	if len(results) != 2 || results[0].Value != "a" || results[1].Err == nil {
		t.Fatal("serial results wrong")
	}
}

func TestMixed(t *testing.T) {
	p := New(WithMaxWorkers(2))
	results := p.Mixed(context.Background(),
		[]Task{func(ctx context.Context) (interface{}, error) { return 1, nil }},
		[]Task{func(ctx context.Context) (interface{}, error) { return 2, nil }},
	)
	if len(results) != 2 || results[0][0].Value != 1 || results[1][0].Value != 2 {
		t.Fatalf("mixed results wrong: %v", results)
	}
}

func TestSubmitAndGet(t *testing.T) {
	p := New(WithMaxWorkers(2))
	f, err := p.Submit(context.Background(), func(ctx context.Context) (interface{}, error) {
		time.Sleep(5 * time.Millisecond)
		return "done", nil
	})
	if err != nil {
		t.Fatalf("submit err: %v", err)
	}
	v, err := f.Get(context.Background())
	if err != nil || v != "done" {
		t.Fatalf("get v=%v err=%v", v, err)
	}
}

func TestSubmitAll(t *testing.T) {
	p := New(WithMaxWorkers(4))
	futures, err := p.SubmitAll(context.Background(),
		func(ctx context.Context) (interface{}, error) { return 1, nil },
		func(ctx context.Context) (interface{}, error) { return 2, nil },
	)
	if err != nil || len(futures) != 2 {
		t.Fatalf("err=%v len=%d", err, len(futures))
	}
	for _, f := range futures {
		if _, err := f.Get(context.Background()); err != nil {
			t.Fatalf("future err: %v", err)
		}
	}
}

func TestDefaultPool(t *testing.T) {
	if Default() == nil {
		t.Fatal("Default() nil")
	}
	res := Go(context.Background(), func(ctx context.Context) (interface{}, error) { return 42, nil })
	if res[0].Value != 42 {
		t.Fatal("Go wrong")
	}
	rs := GoSerial(context.Background(), func(ctx context.Context) (interface{}, error) { return 7, nil })
	if rs[0].Value != 7 {
		t.Fatal("GoSerial wrong")
	}
	gm := GoMixed(context.Background(), []Task{func(ctx context.Context) (interface{}, error) { return 9, nil }})
	if gm[0][0].Value != 9 {
		t.Fatal("GoMixed wrong")
	}
}

func TestStats(t *testing.T) {
	p := New(WithMaxWorkers(2))
	p.Parallel(context.Background(), func(ctx context.Context) (interface{}, error) { return nil, nil })
	s := p.Stats()
	if s.TotalSubmitted != 1 || s.TotalCompleted != 1 {
		t.Fatalf("stats wrong: %+v", s)
	}
	if p.MaxWorkers() != 2 {
		t.Fatal("MaxWorkers wrong")
	}
}

func TestSubmitContextCancel(t *testing.T) {
	p := New(WithMaxWorkers(1))
	_, _ = p.Submit(context.Background(), func(ctx context.Context) (interface{}, error) {
		time.Sleep(50 * time.Millisecond)
		return nil, nil
	})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := p.Submit(ctx, func(ctx context.Context) (interface{}, error) { return nil, nil })
	if err == nil {
		t.Fatal("expected ctx error when pool full and ctx cancelled")
	}
}


