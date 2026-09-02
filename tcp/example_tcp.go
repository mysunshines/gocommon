package tcpclient

import (
	"context"
	"fmt"
	"log"
	"net"
	"time"
)

// 示例TCP服务器（用于测试）
func startTestServer(addr string) net.Listener {
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		log.Fatalf("启动服务器失败: %v", err)
	}

	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				continue
			}

			go func() {
				defer conn.Close()
				buf := make([]byte, 1024)
				for {
					n, err := conn.Read(buf)
					if err != nil {
						return
					}
					// 发送响应
					conn.Write([]byte("响应: " + string(buf[:n])))
				}
			}()
		}
	}()

	return ln
}

// Example_main 是一个示例演示 TCP 客户端的各种用法
func Example_main() {
	ctx := context.Background()

	// 启动测试服务器
	ln := startTestServer(":8099")
	defer ln.Close()
	fmt.Println("测试服务器启动在 :8099")
	time.Sleep(100 * time.Millisecond)

	// ========== 示例1：创建客户端并连接 ==========
	fmt.Println("\n=== 示例1：基础连接 ===")

	client, err := New(":8099",
		WithReadTimeout(3*time.Second),
		WithWriteTimeout(3*time.Second),
	)
	if err != nil {
		log.Fatalf("创建TCP客户端失败: %v", err)
	}
	defer client.Close()

	// ========== 示例2：同步发送请求 ==========
	fmt.Println("=== 示例2：同步发送请求 ===")

	resp, err := client.Send(ctx, []byte("Hello Server"))
	if err != nil {
		log.Printf("发送失败: %v", err)
	} else {
		fmt.Printf("响应: %s\n", string(resp))
	}

	// ========== 示例3：发送并等待响应（带长度前缀） ==========
	fmt.Println("\n=== 示例3：发送带长度前缀的消息 ===")

	resp, err = client.SendWithLength(ctx, []byte("带长度前缀的消息"))
	if err != nil {
		log.Printf("发送失败: %v", err)
	} else {
		fmt.Printf("响应: %s\n", string(resp))
	}

	// ========== 示例4：发送原始消息（不等待响应） ==========
	fmt.Println("\n=== 示例4：发送原始消息 ===")

	err = client.SendRaw(ctx, []byte("不需要响应的消息"))
	if err != nil {
		log.Printf("发送失败: %v", err)
	} else {
		fmt.Println("原始消息发送成功")
	}

	// ========== 示例5：读取响应 ==========
	fmt.Println("\n=== 示例5：读取响应 ===")

	// 先发送一条消息
	client.SendRaw(ctx, []byte("请回复这条消息"))

	resp, err = client.Read(ctx)
	if err != nil {
		log.Printf("读取响应失败: %v", err)
	} else {
		fmt.Printf("读取到响应: %s\n", string(resp))
	}

	// ========== 示例6：发送并关闭连接 ==========
	fmt.Println("\n=== 示例6：发送并关闭连接 ===")

	client2, _ := New(":8099")
	resp, err = client2.SendAndClose(ctx, []byte("最后一条消息"))
	if err != nil {
		log.Printf("发送并关闭失败: %v", err)
	} else {
		fmt.Printf("响应: %s\n", string(resp))
	}

	// ========== 示例7：检查连接状态 ==========
	fmt.Println("\n=== 示例7：连接状态检查 ===")

	isConnected := client.IsConnected()
	fmt.Printf("连接状态: %v\n", isConnected)

	// ========== 示例8：获取底层连接 ==========
	fmt.Println("\n=== 示例8：获取底层连接 ===")

	conn := client.GetConn()
	if conn != nil {
		fmt.Printf("本地地址: %s\n", conn.LocalAddr().String())
		fmt.Printf("远程地址: %s\n", conn.RemoteAddr().String())
	}

	// ========== 示例9：重连 ==========
	fmt.Println("\n=== 示例9：重连机制 ===")

	// 关闭当前连接
	client.Close()
	fmt.Println("已关闭连接")

	// 重新建立连接
	client, err = New(":8099")
	if err != nil {
		log.Printf("重连失败: %v", err)
	} else {
		defer client.Close()
		fmt.Println("重连成功 ✓")

		// 发送消息验证
		resp, _ = client.Send(ctx, []byte("重连后测试"))
		fmt.Printf("重连后响应: %s\n", string(resp))
	}

	fmt.Println("\n所有示例执行完成!")
}
