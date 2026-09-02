package kafka

import (
	"context"
	"fmt"
	"log"
	"time"

	kafkago "github.com/segmentio/kafka-go"
)

// 模拟Kafka Broker地址
var brokers = []string{"localhost:9092"}

// Example_main 是一个示例演示 Kafka 生产者和消费者的各种用法
func Example_main() {
	ctx := context.Background()

	// ========== 示例1：基本消息发送 ==========
	fmt.Println("=== 示例1：创建生产者 ===")

	producer := NewProducer(brokers, "example-topic")
	defer producer.Close()

	// 发送消息
	err := producer.Send(ctx, []byte("key1"), []byte("Hello Kafka!"))
	if err != nil {
		log.Printf("发送消息失败: %v", err)
	} else {
		fmt.Println("消息发送成功")
	}

	// ========== 示例2：批量发送 ==========
	fmt.Println("\n=== 示例2：批量发送消息 ===")

	messages := []kafkago.Message{
		{Key: []byte("user:1"), Value: []byte("用户1的操作")},
		{Key: []byte("user:2"), Value: []byte("用户2的操作")},
		{Key: []byte("user:1"), Value: []byte("用户1的另一次操作")},
	}

	err = producer.SendBatch(ctx, messages)
	if err != nil {
		log.Printf("批量发送失败: %v", err)
	} else {
		fmt.Println("批量消息发送成功")
	}

	// ========== 示例3：带Header的消息 ==========
	fmt.Println("\n=== 示例3：带Header的消息 ===")

	headers := map[string]string{
		"trace-id":  "trace-123456",
		"span-id":   "span-789",
		"source":    "example-service",
		"timestamp": fmt.Sprintf("%d", time.Now().Unix()),
	}

	err = producer.SendWithHeaders(ctx, []byte("key2"), []byte("带Header的消息"), headers)
	if err != nil {
		log.Printf("带Header发送失败: %v", err)
	} else {
		fmt.Println("带Header消息发送成功")
	}

	// ========== 示例4：发送JSON消息 ==========
	fmt.Println("\n=== 示例4：发送JSON消息 ===")

	data := map[string]interface{}{
		"username":  "testuser",
		"action":    "login",
		"timestamp": time.Now().Unix(),
	}

	err = producer.SendJSON(ctx, "user:login", data)
	if err != nil {
		log.Printf("发送JSON失败: %v", err)
	} else {
		fmt.Println("JSON消息发送成功")
	}

	// ========== 示例5：异步发送 ==========
	fmt.Println("\n=== 示例5：异步发送 ===")

	asyncProducer := NewProducer(brokers, "example-topic").
		WithAsync().
		WithBatch(100, 5*time.Second)

	// 发送多条异步消息
	for i := 0; i < 5; i++ {
		key := fmt.Sprintf("async:%d", i)
		value := fmt.Sprintf("异步消息 %d", i)
		asyncProducer.Send(ctx, []byte(key), []byte(value))
	}

	fmt.Println("异步消息已发送（批量处理中）")
	time.Sleep(1 * time.Second)
	asyncProducer.Close()

	// ========== 示例6：创建消费者 ==========
	fmt.Println("\n=== 示例6：创建消费者 ===")

	consumer := NewConsumer(brokers, "example-topic", "example-group")

	// 设置消息处理函数
	consumer.AddHandler(func(ctx context.Context, key, value []byte) error {
		fmt.Printf("收到消息: key=%s, value=%s\n", string(key), string(value))
		return nil
	})

	// 启动消费者（在后台goroutine运行）
	go func() {
		if err := consumer.Start(ctx); err != nil {
			log.Printf("消费者运行出错: %v", err)
		}
	}()

	fmt.Println("消费者已启动，正在等待消息...")

	// 发送一些测试消息供消费者接收
	time.Sleep(100 * time.Millisecond)
	producer.Send(ctx, []byte("test1"), []byte("测试消息1"))
	producer.Send(ctx, []byte("test2"), []byte("测试消息2"))

	// 等待消费者处理
	time.Sleep(2 * time.Second)

	// ========== 示例7：停止消费者 ==========
	fmt.Println("\n=== 示例7：停止消费者 ===")

	consumer.Stop()
	fmt.Println("消费者已停止")

	// ========== 示例8：使用Channel接收消息 ==========
	fmt.Println("\n=== 示例8：使用Channel接收消息 ===")

	consumer2 := NewConsumer(brokers, "example-topic", "group-channel")

	msgChan, err := consumer2.StartWithChannel(ctx)
	if err != nil {
		log.Printf("启动Channel消费者失败: %v", err)
	} else {
		go func() {
			for msg := range msgChan {
				fmt.Printf("[Channel] 收到消息: key=%s, value=%s\n", string(msg.Key), string(msg.Value))
			}
		}()

		// 发送测试消息
		time.Sleep(100 * time.Millisecond)
		producer.Send(ctx, []byte("channel1"), []byte("Channel测试消息"))

		// 等待处理
		time.Sleep(1 * time.Second)
		consumer2.Stop()
	}

	// ========== 示例9：获取消费者统计 ==========
	fmt.Println("\n=== 示例9：消费者统计 ===")

	stats := consumer.Stats()
	fmt.Printf("消费者主题: %s\n", stats.Topic)
	fmt.Printf("已消费消息数: %d\n", stats.Messages)
	fmt.Printf("已读取字节数: %d\n", stats.Bytes)
	fmt.Printf("消费者状态: %v\n", stats.Dials)
	fmt.Printf("错误数: %d\n", stats.Errors)

	fmt.Println("\n所有示例执行完成!")
}
