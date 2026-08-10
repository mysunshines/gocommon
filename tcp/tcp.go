package tcpclient

import (
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"sync"
	"time"

	"github.com/mysunshines/gocommon/constants"
	"github.com/mysunshines/gocommon/resilience"
)

// Client TCP 客户端
type Client struct {
	address        string        // 远端地址 host:port
	conn           net.Conn      // TCP 连接
	connMu         sync.RWMutex  // 保护 conn 的并发读写
	readTimeout    time.Duration // 读超时（0 表示不限）
	writeTimeout   time.Duration // 写超时（0 表示不限）
	keepAlive      bool          // 是否启用 TCP KeepAlive
	resilienceKey  string        // resilience serviceKey；非空时其 Timeout 覆盖下方读写超时
}

// Config 连接配置
type Config struct {
	Address      string        // 服务器地址
	ReadTimeout  time.Duration // 读取超时
	WriteTimeout time.Duration // 写入超时
	KeepAlive    bool          // 保活连接
}

// Option 配置选项
type Option func(*Client)

// New 创建 TCP 客户端
func New(address string, opts ...Option) (*Client, error) {
	c := &Client{
		address:      address,
		readTimeout:  constants.DefaultReadTimeout * time.Second,
		writeTimeout: constants.DefaultWriteTimeout * time.Second,
		keepAlive:    true,
	}

	for _, opt := range opts {
		opt(c)
	}

	// 建立连接
	if err := c.connect(); err != nil {
		return nil, err
	}

	return c, nil
}

// WithReadTimeout 设置读取超时
func WithReadTimeout(timeout time.Duration) Option {
	return func(c *Client) {
		c.readTimeout = timeout
	}
}

// WithWriteTimeout 设置写入超时
func WithWriteTimeout(timeout time.Duration) Option {
	return func(c *Client) {
		c.writeTimeout = timeout
	}
}

// WithKeepAlive 设置保活
func WithKeepAlive(keepAlive bool) Option {
	return func(c *Client) {
		c.keepAlive = keepAlive
	}
}

// WithResilienceKey 设置 resilience 的 serviceKey；非空时 resilience 策略的 Timeout
// 会覆盖客户端自身的 read/write 超时，实现按下游动态调超时（配合 resilience.SetPolicy）。
func WithResilienceKey(key string) Option {
	return func(c *Client) {
		c.resilienceKey = key
	}
}

// applyDeadlines 统一设置读写截止时间：若设置了 resilienceKey，则优先使用
// resilience 策略的 Timeout 覆盖客户端自身的读写超时，实现按下游动态调超时。
func (c *Client) applyDeadlines(conn net.Conn) {
	readTO, writeTO := c.readTimeout, c.writeTimeout
	if c.resilienceKey != "" {
		if p := resilience.ForService(c.resilienceKey); p.Timeout > 0 {
			readTO, writeTO = p.Timeout, p.Timeout
		}
	}
	if readTO > 0 {
		conn.SetReadDeadline(time.Now().Add(readTO))
	}
	if writeTO > 0 {
		conn.SetWriteDeadline(time.Now().Add(writeTO))
	}
}

// connect 建立连接
func (c *Client) connect() error {
	conn, err := net.DialTimeout("tcp", c.address, constants.DefaultDialTimeout*time.Second)
	if err != nil {
		return fmt.Errorf("failed to connect to %s: %w", c.address, err)
	}

	if c.keepAlive {
		if tcpConn, ok := conn.(*net.TCPConn); ok {
			tcpConn.SetKeepAlive(true)
			tcpConn.SetKeepAlivePeriod(constants.DefaultKeepAlivePeriod * time.Second)
		}
	}

	c.conn = conn
	return nil
}

// reconnect 重新连接
func (c *Client) reconnect() error {
	c.connMu.Lock()
	defer c.connMu.Unlock()

	// 关闭旧连接
	if c.conn != nil {
		c.conn.Close()
	}

	return c.connect()
}

// Send 发送数据
func (c *Client) Send(ctx context.Context, data []byte) ([]byte, error) {
	c.connMu.RLock()
	conn := c.conn
	c.connMu.RUnlock()

	if conn == nil {
		return nil, fmt.Errorf("connection is nil")
	}

	// 设置超时
	c.applyDeadlines(conn)

	// 发送数据
	_, err := conn.Write(data)
	if err != nil {
		// 尝试重连
		if reconnErr := c.reconnect(); reconnErr != nil {
			return nil, fmt.Errorf("send failed and reconnect failed: %w", err)
		}
		return nil, fmt.Errorf("send failed: %w", err)
	}

	// 读取响应（循环读取直到 EOF，已有 readDeadline 兜底）
	resp, err := io.ReadAll(conn)
	if err != nil {
		return nil, fmt.Errorf("read response failed: %w", err)
	}

	return resp, nil
}

// SendWithLength 发送带长度前缀的数据（自定义协议）
func (c *Client) SendWithLength(ctx context.Context, data []byte) ([]byte, error) {
	c.connMu.RLock()
	conn := c.conn
	c.connMu.RUnlock()

	if conn == nil {
		return nil, fmt.Errorf("connection is nil")
	}

	// 设置超时
	c.applyDeadlines(conn)

	// 构造长度前缀消息
	length := uint32(len(data))
	lengthBytes := make([]byte, constants.MsgLenPrefixSize)
	binary.BigEndian.PutUint32(lengthBytes, length)
	message := append(lengthBytes, data...)

	// 发送数据
	_, err := conn.Write(message)
	if err != nil {
		if reconnErr := c.reconnect(); reconnErr != nil {
			return nil, fmt.Errorf("send failed and reconnect failed: %w", err)
		}
		return nil, fmt.Errorf("send failed: %w", err)
	}

	// 读取响应长度
	lengthBuf := make([]byte, constants.MsgLenPrefixSize)
	_, err = io.ReadFull(conn, lengthBuf)
	if err != nil {
		return nil, fmt.Errorf("read response length failed: %w", err)
	}
	respLen := binary.BigEndian.Uint32(lengthBuf)

	// 读取响应数据
	resp := make([]byte, respLen)
	_, err = io.ReadFull(conn, resp)
	if err != nil {
		return nil, fmt.Errorf("read response data failed: %w", err)
	}

	return resp, nil
}

// SendAndClose 发送数据后关闭连接
func (c *Client) SendAndClose(ctx context.Context, data []byte) ([]byte, error) {
	resp, err := c.Send(ctx, data)
	if err != nil {
		return nil, err
	}
	c.Close()
	return resp, nil
}

// SendRaw 发送原始数据（不等待响应）
func (c *Client) SendRaw(ctx context.Context, data []byte) error {
	c.connMu.RLock()
	conn := c.conn
	c.connMu.RUnlock()

	if conn == nil {
		return fmt.Errorf("connection is nil")
	}

	c.applyDeadlines(conn)

	_, err := conn.Write(data)
	if err != nil {
		return fmt.Errorf("send raw data failed: %w", err)
	}

	return nil
}

// Read 读取数据（不发送）
func (c *Client) Read(ctx context.Context) ([]byte, error) {
	c.connMu.RLock()
	conn := c.conn
	c.connMu.RUnlock()

	if conn == nil {
		return nil, fmt.Errorf("connection is nil")
	}

	c.applyDeadlines(conn)

	resp, err := io.ReadAll(conn)
	if err != nil {
		return nil, fmt.Errorf("read failed: %w", err)
	}

	return resp, nil
}

// IsConnected 检查连接状态
func (c *Client) IsConnected() bool {
	c.connMu.RLock()
	defer c.connMu.RUnlock()

	if c.conn == nil {
		return false
	}

	// 检查连接是否可用
	c.conn.SetReadDeadline(time.Now().Add(constants.ConnProbeTimeout * time.Millisecond))
	one := make([]byte, 1)
	_, err := c.conn.Read(one)
	if err != nil {
		return false
	}

	return true
}

// Close 关闭连接
func (c *Client) Close() error {
	c.connMu.Lock()
	defer c.connMu.Unlock()

	if c.conn != nil {
		err := c.conn.Close()
		c.conn = nil
		return err
	}
	return nil
}

// GetConn 获取底层连接
func (c *Client) GetConn() net.Conn {
	c.connMu.RLock()
	defer c.connMu.RUnlock()
	return c.conn
}
