package observability

import (
	"context"
	"testing"

	"github.com/mysunshines/gocommon/config"
)

func TestTraceIDFromContextEmpty(t *testing.T) {
	if got := TraceIDFromContext(context.Background()); got != "" {
		t.Errorf("未初始化时应返回空串，got %q", got)
	}
}

func TestShutdownNil(t *testing.T) {
	if err := Shutdown(context.Background()); err != nil {
		t.Errorf("未初始化时 Shutdown 应返回 nil，got %v", err)
	}
}

func TestInitDisabled(t *testing.T) {
	shutdown, err := Init("svc", config.OTelConfig{Enabled: false})
	if err != nil {
		t.Fatal(err)
	}
	if shutdown == nil {
		t.Fatal("shutdown 不应为 nil")
	}
	if err := shutdown(context.Background()); err != nil {
		t.Errorf("disabled 下 shutdown 应返回 nil，got %v", err)
	}
}

func TestInitAndRegisterSafe(t *testing.T) {
	InitAndRegister("svc", config.OTelConfig{Enabled: false})
	ShutdownGlobal(context.Background())
}
