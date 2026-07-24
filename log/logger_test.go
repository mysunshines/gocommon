package log

import (
	"testing"
)

func TestLogger(t *testing.T) {
	// 使用临时目录，避免污染工作区
	Init(t.TempDir(), "info", "test-service")

	if GetLogger() == nil {
		t.Fatal("GetLogger returned nil")
	}

	// 各级别日志方法不应 panic
	Info("info msg")
	Infof("info %d", 1)
	Debug("debug")
	Debugf("debug %d", 2)
	Warn("warn")
	Warnf("warn %d", 3)
	Error("error")
	Errorf("error %d", 4)

	if WithField("k", "v") == nil {
		t.Fatal("WithField returned nil")
	}
	if WithFields(map[string]interface{}{"a": 1}) == nil {
		t.Fatal("WithFields returned nil")
	}

	// 同一天触发轮转应直接返回 nil
	if err := RotateLog("test-service"); err != nil {
		t.Fatalf("RotateLog err: %v", err)
	}

	StopRotation()
}
