package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestApplyDefaults(t *testing.T) {
	var c Config
	ApplyDefaults(&c)
	if c.App.Env != "development" || c.App.LogLevel != "info" || c.App.LogDir != "./logs" {
		t.Fatalf("app defaults wrong: %+v", c.App)
	}
	if c.App.Host != "0.0.0.0" || c.HTTP.Host != "0.0.0.0" || c.HTTP.Port != 8080 {
		t.Fatalf("http defaults wrong: %+v", c.HTTP)
	}
	if c.GRPC.Host != "0.0.0.0" || c.GRPC.Port != 9000 {
		t.Fatalf("grpc defaults wrong: %+v", c.GRPC)
	}
	if c.Database.MaxOpenConns != 25 || c.Database.MaxIdleConns != 5 || c.Database.ConnMaxLifetime != 3600 {
		t.Fatalf("db defaults wrong: %+v", c.Database)
	}
	if c.Redis.PoolSize != 100 {
		t.Fatalf("redis pool size = %d", c.Redis.PoolSize)
	}
	if c.JWT.ExpireTime != 86400*7 {
		t.Fatalf("jwt expire = %d", c.JWT.ExpireTime)
	}
	if c.Consul.Address != "localhost:8500" || c.Consul.CheckInterval != 10 || c.Consul.DeregisterCritical != 30 {
		t.Fatalf("consul defaults wrong: %+v", c.Consul)
	}
	if c.Metrics.Port != 9090 || c.Metrics.Path != "/metrics" {
		t.Fatalf("metrics defaults wrong: %+v", c.Metrics)
	}
	if c.RateLimit.QPS != 1000 || c.RateLimit.Burst != 2000 {
		t.Fatalf("ratelimit defaults wrong: %+v", c.RateLimit)
	}
}

func TestAddr(t *testing.T) {
	a := AppConfig{Host: "1.2.3.4", Port: 8080}
	if a.Addr() != "1.2.3.4:8080" {
		t.Fatalf("AppConfig.Addr = %s", a.Addr())
	}
	// 使用 ApplyDefaults 后的默认值，App.Port 未设置（0）时 Addr 仅返回 host
	var def Config
	ApplyDefaults(&def)
	if def.App.Addr() != "0.0.0.0" {
		t.Fatalf("default AppConfig.Addr = %s", def.App.Addr())
	}
	def.App.Port = 8080
	if def.App.Addr() != "0.0.0.0:8080" {
		t.Fatalf("AppConfig.Addr with port = %s", def.App.Addr())
	}
	d := DatabaseConfig{Host: "localhost", Port: 3306, User: "u", Password: "p", Name: "db"}
	if d.DSN() != "u:p@tcp(localhost:3306)/db?charset=utf8mb4&parseTime=True&loc=Local" {
		t.Fatalf("DSN = %s", d.DSN())
	}
	r := RedisConfig{Host: "127.0.0.1", Port: 6379}
	if r.Addr() != "127.0.0.1:6379" {
		t.Fatalf("RedisConfig.Addr = %s", r.Addr())
	}
	g := GRPCConfig{Host: "0.0.0.0", Port: 9000}
	if g.Addr() != "0.0.0.0:9000" {
		t.Fatalf("GRPCConfig.Addr = %s", g.Addr())
	}
	h := HTTPConfig{Host: "0.0.0.0", Port: 8080}
	if h.Addr() != "0.0.0.0:8080" {
		t.Fatalf("HTTPConfig.Addr = %s", h.Addr())
	}
}

func TestLoad(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	content := "app:\n  name: test-svc\n  env: production\n  port: 9090\nredis:\n  host: redis.local\n  port: 6380\n"
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	c, err := Load(path)
	if err != nil {
		t.Fatalf("Load err: %v", err)
	}
	if c.App.Name != "test-svc" || c.App.Env != "production" || c.App.Port != 9090 {
		t.Fatalf("parsed app wrong: %+v", c.App)
	}
	if c.App.LogLevel != "info" || c.HTTP.Port != 8080 {
		t.Fatalf("defaults not applied: %+v", c)
	}
	if c.Redis.Host != "redis.local" || c.Redis.Port != 6380 {
		t.Fatalf("parsed redis wrong: %+v", c.Redis)
	}
	if Get() != c {
		t.Fatal("Get() should return last loaded config")
	}
}

func TestLoadMissingFile(t *testing.T) {
	if _, err := Load("/nonexistent/config.yaml"); err == nil {
		t.Fatal("expected error for missing file")
	}
}

func TestLoadFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "c.yaml")
	if err := os.WriteFile(path, []byte("foo: bar\nnum: 7\n"), 0644); err != nil {
		t.Fatal(err)
	}
	var out struct {
		Foo string `yaml:"foo"`
		Num int    `yaml:"num"`
	}
	if err := LoadFile(path, &out); err != nil {
		t.Fatalf("LoadFile err: %v", err)
	}
	if out.Foo != "bar" || out.Num != 7 {
		t.Fatalf("LoadFile parsed wrong: %+v", out)
	}
}

func TestApplyEnvOverrides(t *testing.T) {
	var c Config
	ApplyDefaults(&c)
	t.Setenv("DB_HOST", "env-host")
	t.Setenv("DB_PORT", "9999")
	t.Setenv("REDIS_HOST", "env-redis")
	t.Setenv("CONSUL_ADDRESS", "env-consul:8500")
	ApplyEnvOverrides(&c)
	if c.Database.Host != "env-host" || c.Database.Port != 9999 {
		t.Fatalf("DB env override wrong: %+v", c.Database)
	}
	if c.Redis.Host != "env-redis" {
		t.Fatalf("Redis env override wrong: %+v", c.Redis)
	}
	if c.Consul.Address != "env-consul:8500" {
		t.Fatalf("Consul env override wrong: %+v", c.Consul)
	}
}
