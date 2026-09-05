package kafka

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/mysunshines/gocommon/constants"
	"github.com/mysunshines/gocommon/log"
	"github.com/mysunshines/gocommon/middleware"

	"github.com/segmentio/kafka-go"
	"github.com/sirupsen/logrus"
)

// Producer Kafka 生产者
type Producer struct {
	writer *kafka.Writer // Kafka 写入器（批量发送）
	mu     sync.RWMutex  // 保护 writer 并发写
}

// Consumer Kafka 消费者
type Consumer struct {
	reader   *kafka.Reader    // Kafka 读取器
	handlers []MessageHandler // 消息处理器列表
	mu       sync.RWMutex     // 保护 handlers/running 并发读写
	running  bool             // 消费循环是否正在运行
	stopCh   chan struct{}    // 停止信号，关闭后退出消费循环
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
	traceID := middleware.GetTraceIDFromContext(ctx)
	if err := p.writer.WriteMessages(ctx, msg); err != nil {
		log.WithFields(logrus.Fields{
			constants.LogFieldTraceID: traceID,
			"topic":                   p.writer.Topic,
			"err":                     err.Error(),
		}).Errorf("[kafka] send message failed")
		return err
	}
	log.WithFields(logrus.Fields{
		constants.LogFieldTraceID: traceID,
		"topic":                   p.writer.Topic,
	}).Debugf("[kafka] message sent")
	return nil
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
	traceID := middleware.GetTraceIDFromContext(ctx)
	if err := p.writer.WriteMessages(ctx, msg); err != nil {
		log.WithFields(logrus.Fields{
			constants.LogFieldTraceID: traceID,
			"topic":                   p.writer.Topic,
			"err":                     err.Error(),
		}).Errorf("[kafka] send message with headers failed")
		return err
	}
	log.WithFields(logrus.Fields{
		constants.LogFieldTraceID: traceID,
		"topic":                   p.writer.Topic,
	}).Debugf("[kafka] message sent")
	return nil
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

	traceID := middleware.GetTraceIDFromContext(ctx)
	if err := p.writer.WriteMessages(ctx, messages...); err != nil {
		log.WithFields(logrus.Fields{
			constants.LogFieldTraceID: traceID,
			"topic":                   p.writer.Topic,
			"count":                   len(messages),
			"err":                     err.Error(),
		}).Errorf("[kafka] send batch failed")
		return err
	}
	log.WithFields(logrus.Fields{
		constants.LogFieldTraceID: traceID,
		"topic":                   p.writer.Topic,
		"count":                   len(messages),
	}).Debugf("[kafka] batch sent")
	return nil
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
					log.WithFields(logrus.Fields{
						constants.LogFieldTraceID: middleware.GetTraceIDFromContext(ctx),
						"topic":                   c.reader.Config().Topic,
						"partition":               msg.Partition,
						"offset":                  msg.Offset,
						"err":                     err.Error(),
					}).Errorf("[kafka] message handler error")
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
	Topic     string            // 所属主题
	Partition int               // 分区编号
	Offset    int64             // 分区内偏移量
	Key       []byte            // 消息键（用于分区路由）
	Value     []byte            // 消息体
	Headers   map[string]string // 消息头
	Time      time.Time         // 消息时间戳
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
	Handler MessageHandler // 实际消息处理逻辑
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
				log.WithFields(logrus.Fields{
					constants.LogFieldTraceID: middleware.GetTraceIDFromContext(ctx),
					"err":                     err.Error(),
				}).Errorf("[kafka] consumer group handler error")
			}
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}
