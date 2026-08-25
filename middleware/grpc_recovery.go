package middleware

import (
	"context"
	"runtime/debug"

	"github.com/mysunshines/gocommon/log"
	"github.com/mysunshines/gocommon/metrics"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// GRPCRecoveryInterceptor 返回 gRPC 一元服务端拦截器，等价于 HTTP 层的
// RecoveryMiddleware：捕获业务 handler 中的 panic，记录指标与日志后转为
// Internal 错误返回，防止 panic 击穿整个进程。
//
// 为什么必需：gRPC-go 默认不恢复 handler 的 panic——任一 handler panic 都会
// 导致整个服务进程崩溃（连带拖垮同进程的其它请求）。挂载本拦截器后，
// 单个请求的 panic 只影响该请求本身。
//
// 挂载位置：建议放在拦截器链首位，使其包裹（含其它拦截器在内的）整条链路；
// service 为服务名（如 constants.ServiceNameArticle），用于 panic_counter_total 打标。
//
// 行为：
//   - 指标：metrics.RecordPanic(service) → panic_counter_total{service} +1
//     （Grafana Infrastructure Overview 的 Panic 面板数据来源）；
//   - 日志：打印方法名、panic 值与完整调用栈；
//   - 响应：返回 codes.Internal，客户端看到 "internal server error"，
//     不泄露 panic 细节（防信息泄露）。
func GRPCRecoveryInterceptor(service string) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo,
		handler grpc.UnaryHandler) (resp interface{}, err error) {
		// 命名返回值：defer 中需改写 resp/err，转换为 Internal 错误
		defer func() {
			if r := recover(); r != nil {
				metrics.RecordPanic(service)
				log.Errorf("[gRPC-Panic] service=%s method=%s panic=%v\n%s",
					service, info.FullMethod, r, debug.Stack())
				resp = nil
				err = status.Error(codes.Internal, "internal server error")
			}
		}()
		return handler(ctx, req)
	}
}
