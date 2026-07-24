package udpclient

import (
	"context"
	"fmt"
	"log"
	"net"
	"time"
)

// Example_main 是一个示例演示 UDP 客户端的各种用法
func Example_main() {
	ctx := context.Background()

	// ========== 示例1：创建UDP客户端 ==========
	fmt.Println("=== 示例1：基础UDP客户端 ===")

	// 创建连接到测试服务器（将在后面启动）
	client, err := New(":0", WithLocalAddress("127.0.0.1"))
	if err != nil {
		log.Fatalf("创建UDP客户端失败: %v", err)
	}
	defer client.Close()
	fmt.Println("UDP客户端创建成功")

	// ========== 示例2：发送消息 ==========
	fmt.Println("\n=== 示例2：发送消息 ===")

	// 启动一个简单的UDP服务器用于测试
	go startUDPServer(":9099")
	time.Sleep(100 * time.Millisecond)

	err = client.SendTo(ctx, "localhost:9099", []byte("Hello UDP Server"))
	if err != nil {
		log.Printf("发送失败: %v", err)
	} else {
		fmt.Println("消息已发送")
	}

	// ========== 示例3：发送并等待响应 ==========
	fmt.Println("\n=== 示例3：发送并等待响应 ===")

	// 创建连接到服务器
	client2, _ := New("localhost:9099")

	resp, err := client2.SendAndReceive(ctx, []byte("需要响应"))
	if err != nil {
		log.Printf("发送并等待响应失败: %v", err)
	} else {
		fmt.Printf("收到响应: %s\n", string(resp))
	}
	client2.Close()

	// ========== 示例4：广播消息 ==========
	fmt.Println("\n=== 示例4：UDP广播 ===")

	// 创建用于广播的客户端
	broadcastClient, err := New(":0", WithLocalAddress("127.0.0.1"))
	if err != nil {
		log.Printf("创建广播客户端失败: %v", err)
	} else {
		defer broadcastClient.Close()

		// 广播到同一网络的广播地址
		err = broadcastClient.Broadcast(ctx, 9099, []byte("广播消息"))
		if err != nil {
			log.Printf("广播失败: %v", err)
		} else {
			fmt.Println("广播消息已发送")
		}
	}

	// ========== 示例5：组播 ==========
	fmt.Println("\n=== 示例5：UDP组播 ===")

	multicastClient, err := New(":0")
	if err != nil {
		log.Printf("创建组播客户端失败: %v", err)
	} else {
		defer multicastClient.Close()

		// 发送到组播地址
		err = multicastClient.Multicast(ctx, "239.255.255.250:1900", []byte("组播消息"))
		if err != nil {
			log.Printf("组播失败: %v", err)
		} else {
			fmt.Println("组播消息已发送")
		}
	}

	// ========== 示例6：接收消息 ==========
	fmt.Println("\n=== 示例6：接收UDP消息 ===")

	// 创建用于接收的服务器连接
	serverConn, err := ServerConn("localhost:9098")
	if err != nil {
		log.Fatalf("创建服务器连接失败: %v", err)
	}

	// 创建用于接收的客户端
	rxClient := &Client{conn: serverConn}
	defer rxClient.Close()

	// 启动接收goroutine
	go func() {
		for {
			data, addr, err := rxClient.Receive(ctx)
			if err != nil {
				log.Printf("接收消息出错: %v", err)
				continue
			}
			fmt.Printf("收到来自 %s 的消息: %s\n", addr, string(data))

			// 回复
			rxClient.SendTo(ctx, addr.String(), []byte("已收到"))
		}
	}()

	// 发送消息到接收客户端
	time.Sleep(100 * time.Millisecond)
	err = client.SendTo(ctx, "localhost:9098", []byte("测试消息"))
	if err != nil {
		log.Printf("发送消息到9098失败: %v", err)
	}

	// 等待接收
	time.Sleep(1 * time.Second)

	// ========== 示例7：异步发送 ==========
	fmt.Println("\n=== 示例7：异步发送 ===")

	client3, _ := New("localhost:9099")
	defer client3.Close()

	// 异步发送多条消息
	client3.SendAsync(ctx, []byte("异步消息1"))
	client3.SendAsync(ctx, []byte("异步消息2"))
	client3.SendAsync(ctx, []byte("异步消息3"))

	fmt.Println("异步消息已发送")

	// ========== 示例8：获取客户端信息 ==========
	fmt.Println("\n=== 示例8：客户端状态 ===")

	fmt.Printf("本地地址: %s\n", client.LocalAddr())
	fmt.Printf("连接状态: %v\n", client.IsConnected())

	conn := client.GetConn()
	if conn != nil {
		fmt.Printf("底层连接本地地址: %s\n", conn.LocalAddr().String())
	}

	fmt.Println("\n所有示例执行完成!")
}

// startUDPServer 启动简单的UDP服务器用于测试
func startUDPServer(addr string) {
	pc, err := net.ListenPacket("udp", addr)
	if err != nil {
		log.Printf("启动UDP服务器失败: %v", err)
		return
	}
	defer pc.Close()

	fmt.Printf("UDP测试服务器启动在 %s\n", addr)

	buf := make([]byte, 1024)
	for {
		n, addr, err := pc.ReadFrom(buf)
		if err != nil {
			continue
		}
		fmt.Printf("服务器收到: %s (来自 %s)\n", string(buf[:n]), addr)

		// 发送响应
		pc.WriteTo([]byte("响应: "+string(buf[:n])), addr)
	}
}