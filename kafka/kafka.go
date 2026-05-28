package kafka

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/segmentio/kafka-go"
)

// Producer Kafka 生产者
type Producer struct {
	writer *kafka.Writer
	mu     sync.RWMutex
}

// Consumer Kafka 消费者
type Consumer struct {
	reader   *kafka.Reader
	handlers []MessageHandler
	mu       sync.RWMutex
	running  bool
	stopCh   chan struct{}
}

// MessageHandler 消息处理函数
type MessageHandler func(ctx context.Context, key, value []byte) error

// NewProducer 创建 Kafka 生产者
func NewProducer(brokers []string, topic string) *Producer {
	writer := &kafka.Writer{
		Addr:         kafka.TCP(brokers...),
		Topic:        topic,
		Balancer:     &kafka.LeastBytes{},
		BatchSize:    100,
		BatchTimeout: 10 * time.Millisecond,
	}

	return &Producer{writer: writer}
}

// ProducerWithBalancer 设置负载均衡器
func (p *Producer) WithBalancer(balancer string) *Producer {
	switch balancer {
	case "least":
		p.writer.Balancer = &kafka.LeastBytes{}
	case "roundrobin":
		p.writer.Balancer = &kafka.RoundRobin{}
	case "hash":
		p.writer.Balancer = &kafka.Hash{}
	default:
		p.writer.Balancer = &kafka.LeastBytes{}
	}
	return p
}

// ProducerWithBatch 设置批处理
func (p *Producer) WithBatch(size int, timeout time.Duration) *Producer {
	p.writer.BatchSize = size
	p.writer.BatchTimeout = timeout
	return p
}

// ProducerWithAsync 设置异步模式
func (p *Producer) WithAsync() *Producer {
	p.writer.Async = true
	return p
}

// Send 发送消息
func (p *Producer) Send(ctx context.Context, key, value []byte) error {
	p.mu.RLock()
	defer p.mu.RUnlock()

	msg := kafka.Message{
		Key:   key,
		Value: value,
		Time:  time.Now(),
	}

	return p.writer.WriteMessages(ctx, msg)
}

// SendWithHeaders 发送带 Header 的消息
func (p *Producer) SendWithHeaders(ctx context.Context, key, value []byte, headers map[string]string) error {
	p.mu.RLock()
	defer p.mu.RUnlock()

	hdrs := make([]kafka.Header, 0, len(headers))
	for k, v := range headers {
		hdrs = append(hdrs, kafka.Header{Key: k, Value: []byte(v)})
	}

	msg := kafka.Message{
		Key:     key,
		Value:   value,
		Headers: hdrs,
		Time:    time.Now(),
	}

	return p.writer.WriteMessages(ctx, msg)
}

// SendJSON 发送 JSON 消息
func (p *Producer) SendJSON(ctx context.Context, key string, data interface{}) error {
	value, err := json.Marshal(data)
	if err != nil {
		return fmt.Errorf("failed to marshal json: %w", err)
	}
	return p.Send(ctx, []byte(key), value)
}

// SendBatch 批量发送消息
func (p *Producer) SendBatch(ctx context.Context, messages []kafka.Message) error {
	p.mu.RLock()
	defer p.mu.RUnlock()

	return p.writer.WriteMessages(ctx, messages...)
}

// Close 关闭生产者
func (p *Producer) Close() error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.writer != nil {
		return p.writer.Close()
	}
	return nil
}

// NewConsumer 创建 Kafka 消费者
func NewConsumer(brokers []string, topic, groupID string) *Consumer {
	reader := kafka.NewReader(kafka.ReaderConfig{
		Brokers: brokers,
		Topic:   topic,
		GroupID: groupID,
	})

	return &Consumer{
		reader:   reader,
		handlers: make([]MessageHandler, 0),
		stopCh:   make(chan struct{}),
	}
}

// AddHandler 添加消息处理器
func (c *Consumer) AddHandler(handler MessageHandler) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.handlers = append(c.handlers, handler)
}

// Start 开始消费
func (c *Consumer) Start(ctx context.Context) error {
	c.mu.Lock()
	if c.running {
		c.mu.Unlock()
		return fmt.Errorf("consumer already running")
	}
	c.running = true
	c.mu.Unlock()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-c.stopCh:
			return nil
		default:
			msg, err := c.reader.ReadMessage(ctx)
			if err != nil {
				if ctx.Err() != nil {
					return ctx.Err()
				}
				continue
			}

			// 处理消息
			c.mu.RLock()
			handlers := c.handlers
			c.mu.RUnlock()

			for _, handler := range handlers {
				if err := handler(ctx, msg.Key, msg.Value); err != nil {
					fmt.Printf("message handler error: %v\n", err)
				}
			}
		}
	}
}

// StartWithChannel 使用通道消费
func (c *Consumer) StartWithChannel(ctx context.Context) (<-chan kafka.Message, error) {
	c.mu.Lock()
	if c.running {
		c.mu.Unlock()
		return nil, fmt.Errorf("consumer already running")
	}
	c.running = true
	c.mu.Unlock()

	msgCh := make(chan kafka.Message, 100)

	go func() {
		defer close(msgCh)
		for {
			select {
			case <-ctx.Done():
				return
			case <-c.stopCh:
				return
			default:
				msg, err := c.reader.ReadMessage(ctx)
				if err != nil {
					if ctx.Err() != nil {
						return
					}
					continue
				}
				select {
				case msgCh <- msg:
				case <-ctx.Done():
					return
				case <-c.stopCh:
					return
				}
			}
		}
	}()

	return msgCh, nil
}

// Stop 停止消费
func (c *Consumer) Stop() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if !c.running {
		return nil
	}

	close(c.stopCh)
	c.running = false

	if c.reader != nil {
		return c.reader.Close()
	}
	return nil
}

// Stats 获取消费统计
func (c *Consumer) Stats() kafka.ReaderStats {
	return c.reader.Stats()
}

// Message Kafka 消息结构
type Message struct {
	Topic     string
	Partition int
	Offset    int64
	Key       []byte
	Value     []byte
	Headers   map[string]string
	Time      time.Time
}

// ParseMessage 解析 Kafka 消息
func ParseMessage(msg kafka.Message) *Message {
	headers := make(map[string]string)
	for _, h := range msg.Headers {
		headers[h.Key] = string(h.Value)
	}

	return &Message{
		Topic:     msg.Topic,
		Partition: msg.Partition,
		Offset:    msg.Offset,
		Key:       msg.Key,
		Value:     msg.Value,
		Headers:   headers,
		Time:      msg.Time,
	}
}

// ConsumerGroupHandler 消费者组处理器接口
type ConsumerGroupHandler interface {
	Setup(ctx context.Context) error
	Cleanup(ctx context.Context) error
	ConsumeClaim(ctx context.Context, messages <-chan kafka.Message) error
}

// SimpleHandler 简单消息处理器
type SimpleHandler struct {
	Handler MessageHandler
}

// Setup 实现 ConsumerGroupHandler
func (h *SimpleHandler) Setup(ctx context.Context) error {
	return nil
}

// Cleanup 实现 ConsumerGroupHandler
func (h *SimpleHandler) Cleanup(ctx context.Context) error {
	return nil
}

// ConsumeClaim 实现 ConsumerGroupHandler
func (h *SimpleHandler) ConsumeClaim(ctx context.Context, messages <-chan kafka.Message) error {
	for {
		select {
		case msg, ok := <-messages:
			if !ok {
				return nil
			}
			if err := h.Handler(ctx, msg.Key, msg.Value); err != nil {
				fmt.Printf("consumer group handler error: %v\n", err)
			}
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}
