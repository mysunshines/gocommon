package middleware

import (
	"context"
	"errors"
	"testing"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// TestGRPCRecoveryInterceptor_CatchesPanic 验证 handler panic 被拦截并转为 Internal，
// 进程不崩溃（测试本身能跑完即是证明）。
func TestGRPCRecoveryInterceptor_CatchesPanic(t *testing.T) {
	ic := GRPCRecoveryInterceptor("test-service")
	info := &grpc.UnaryServerInfo{FullMethod: "/test.v1.Test/Panicky"}

	resp, err := ic(context.Background(), nil, info,
		func(ctx context.Context, req interface{}) (interface{}, error) {
			panic("boom")
		})

	if resp != nil {
		t.Fatalf("expected nil resp, got %v", resp)
	}
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if status.Code(err) != codes.Internal {
		t.Fatalf("expected Internal, got %v", status.Code(err))
	}
	// 不泄露 panic 细节
	if status.Convert(err).Message() != "internal server error" {
		t.Fatalf("message should not leak panic detail, got %q", status.Convert(err).Message())
	}
}

// TestGRPCRecoveryInterceptor_PassesThrough 验证正常请求不受影响（resp/err 原样透传）。
func TestGRPCRecoveryInterceptor_PassesThrough(t *testing.T) {
	ic := GRPCRecoveryInterceptor("test-service")
	info := &grpc.UnaryServerInfo{FullMethod: "/test.v1.Test/Ok"}

	want := "payload"
	sentinel := errors.New("biz error")

	// 成功路径
	resp, err := ic(context.Background(), nil, info,
		func(ctx context.Context, req interface{}) (interface{}, error) {
			return want, nil
		})
	if resp != want || err != nil {
		t.Fatalf("success path: resp=%v err=%v", resp, err)
	}

	// 业务错误路径（error 不应被改写为 Internal）
	_, err = ic(context.Background(), nil, info,
		func(ctx context.Context, req interface{}) (interface{}, error) {
			return nil, sentinel
		})
	if !errors.Is(err, sentinel) {
		t.Fatalf("error path: expected sentinel, got %v", err)
	}
}
