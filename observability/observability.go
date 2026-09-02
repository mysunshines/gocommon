// Package observability 集中封装 OpenTelemetry 链路追踪的初始化与取数。
//
// 设计目标：用 OpenTelemetry 的 TraceID 作为全链路唯一 ID，替换 gocommon 早期
// 自研的随机 X-Trace-ID 生成逻辑，从而让「日志（A 方案 / Loki）」与「链路追踪
// （B 方案 / Tempo·Jaeger）」共用同一个 trace_id：
//
//   - 同一请求的 trace_id 既出现在每条结构化日志（Loki 查询用），
//     又能对应到 Tempo/Jaeger 里真实的 span 调用树（OTLP 导出用）。
//
// 兼容性：HTTP 层仍回写 X-Trace-ID 响应头（前端兼容），gRPC 层仍把 trace_id 注入
// metadata（日志串联用）。底层 trace_id 的来源从「自研随机串」升级为「OTel TraceID
// （16 字节 hex）」，对上游调用方完全透明。
//
// 降级：若未调用 InitTracer（或 OTLP 地址为空），TraceFromContext/TraceIDFromContext
// 退化为返回空串，调用方（中间件）会回退到原有逻辑或生成占位 ID，不影响现有部署。
package observability

import (
	"context"
	"fmt"

	"github.com/mysunshines/gocommon/config"

	"go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.21.0"
	"go.opentelemetry.io/otel/trace"
	"google.golang.org/grpc"
)

// tracerProvider 保存全局 TracerProvider 句柄，便于 Shutdown 时优雅关闭 exporter。
var tracerProvider *sdktrace.TracerProvider

// InitTracer 初始化 OpenTelemetry TracerProvider，并将 OTLP 导出器指向 otel-collector。
//
// serviceName 用于资源标识（Tempo/Jaeger 中区分服务），otlpEndpoint 为
// otel-collector 的 gRPC 地址（如 "otel-collector:4317"）。
//
// 当 otlpEndpoint 为空时直接跳过（降级：不采集 trace，但 TraceIDFromContext 仍可用，
// 只是所有 trace id 为空也不影响日志流程——上层中间件会处理空值）。
//
// 返回 shutdown 函数，应在进程退出前调用以 flush 残留 span。
func InitTracer(serviceName, otlpEndpoint string) (func(context.Context) error, error) {
	if otlpEndpoint == "" {
		return func(context.Context) error { return nil }, nil
	}

	exporter, err := otlptracegrpc.New(
		context.Background(),
		otlptracegrpc.WithEndpoint(otlpEndpoint),
		otlptracegrpc.WithInsecure(),
	)
	if err != nil {
		return nil, err
	}

	res, err := resource.New(
		context.Background(),
		resource.WithAttributes(
			semconv.ServiceName(serviceName),
		),
	)
	if err != nil {
		return nil, err
	}

	tp := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exporter),
		sdktrace.WithResource(res),
	)
	otel.SetTracerProvider(tp)

	// 启用 W3C traceparent/tracestate 传播器：跨 HTTP/gRPC 自动串联 span 父子关系，
	// 无需业务手动拼接 metadata（原 x-trace-id 仅保留给日志关联用）。
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	))

	tracerProvider = tp
	return tp.Shutdown, nil
}

// Tracer 返回全局 tracer（按服务名区分）。未初始化时返回 otel 默认 no-op tracer。
func Tracer() trace.Tracer {
	return otel.Tracer("gocommon/observability")
}

// TraceIDFromContext 从 ctx 中提取 OTel 当前的 trace id（16 字节 hex 字符串）。
// 若 ctx 中无 span 或尚未初始化，返回空串（调用方负责回退）。
func TraceIDFromContext(ctx context.Context) string {
	span := trace.SpanFromContext(ctx)
	if !span.SpanContext().IsValid() {
		return ""
	}
	return span.SpanContext().TraceID().String()
}

// Shutdown 优雅关闭 TracerProvider（flush 残留 span 到 collector）。
// 多次调用安全。
func Shutdown(ctx context.Context) error {
	if tracerProvider == nil {
		return nil
	}
	return tracerProvider.Shutdown(ctx)
}

// Init 便捷封装：从 config.OTelConfig 初始化 TracerProvider。
// enabled=false 或 endpoint 为空时跳过（降级：不采集 trace）。
// 服务 main 启动早期调用一次；返回的 shutdown 在进程退出时调用。
func Init(serviceName string, cfg config.OTelConfig) (func(context.Context) error, error) {
	if !cfg.Enabled || cfg.Endpoint == "" {
		return func(context.Context) error { return nil }, nil
	}
	return InitTracer(serviceName, cfg.Endpoint)
}

// GRPCServerOptions 返回 gRPC 服务端启用链路追踪所需的 ServerOption。
// 服务在创建 grpc.NewServer 时追加这些选项，即可让服务端基于入站 W3C traceparent
// 生成子 span，形成完整调用树（想法 3 · 方案 B）。OTel 未初始化时无副作用。
func GRPCServerOptions() []grpc.ServerOption {
	return []grpc.ServerOption{
		grpc.StatsHandler(otelgrpc.NewServerHandler()),
	}
}

// globalShutdown 保存 InitAndRegister 注册的唯一 shutdown 函数，供 ShutdownGlobal 调用。
var globalShutdown func(context.Context) error

// InitAndRegister 等价于 Init，但会把 shutdown 注册为全局（供进程退出时统一 flush）。
// 服务 main 的 run() 启动早期调用即可，无需关心返回值；releaseInfra / 退出兜底处调用
// ShutdownGlobal 即可优雅关闭。降级（未启用）时注册空函数，调用安全。
func InitAndRegister(serviceName string, cfg config.OTelConfig) {
	shutdown, err := Init(serviceName, cfg)
	if err != nil {
		// 初始化失败仅告警，不致命（降级为不采集 trace）。
		fmt.Printf("[observability] init failed for %s: %v\n", serviceName, err)
	}
	globalShutdown = shutdown
}

// ShutdownGlobal 优雅关闭全局 TracerProvider（flush 残留 span）。重复调用安全。
func ShutdownGlobal(ctx context.Context) {
	if globalShutdown == nil {
		return
	}
	_ = globalShutdown(ctx)
	globalShutdown = nil
}
