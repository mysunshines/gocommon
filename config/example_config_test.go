package config

import (
	"fmt"
	"os"
)

func ExampleLoad() {
	f, err := os.CreateTemp("", "config-example-*.yaml")
	if err != nil {
		fmt.Println("create temp:", err)
		return
	}
	defer os.Remove(f.Name())
	if _, err := f.WriteString("app:\n  name: demo\n  env: production\n  port: 8081\nredis:\n  host: 127.0.0.1\n  port: 6379\n"); err != nil {
		fmt.Println("write:", err)
		return
	}
	_ = f.Close()

	c, err := Load(f.Name())
	if err != nil {
		fmt.Println("load:", err)
		return
	}
	fmt.Printf("name=%s env=%s port=%d redis=%s:%d\n", c.App.Name, c.App.Env, c.App.Port, c.Redis.Host, c.Redis.Port)
	// Output: name=demo env=production port=8081 redis=127.0.0.1:6379
}
