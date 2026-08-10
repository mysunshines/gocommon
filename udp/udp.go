package udpclient

import (
	"context"
	"fmt"
	"net"
	"sync"
	"time"

	"github.com/mysunshines/gocommon/constants"
	"github.com/mysunshines/gocommon/resilience"
)

// Client UDP 客户端
type Client struct {
	address        string        // 远端地址 host:port
	localAddr      *net.UDPAddr  // 本地绑定地址（nil 表示由系统分配）
	conn           *net.UDPConn  // UDP 连接
	connMu         sync.RWMutex  // 保护 conn 的并发读写
	readTimeout    time.Duration // 读超时（0 表示不限）
	writeTimeout   time.Duration // 写超时（0 表示不限）
	bufSize        int           // 接收缓冲大小
	resilienceKey  string        // resilience serviceKey；非空时其 Timeout 覆盖读写超时
}

// Config 连接配置
type Config struct {
	Address      string        // 服务器地址
	LocalAddress string        // 本地地址（可选）
	ReadTimeout  time.Duration // 读取超时
	WriteTimeout time.Duration // 写入超时
	BufferSize   int           // 缓冲区大小
}

// Option 配置选项
type Option func(*Client)

// New 创建 UDP 客户端
func New(address string, opts ...Option) (*Client, error) {
	c := &Client{
		address:      address,
		readTimeout:  constants.DefaultReadTimeout * time.Second,
		writeTimeout: constants.DefaultWriteTimeout * time.Second,
		bufSize:      constants.DefaultNetBufSize,
	}

	for _, opt := range opts {
		opt(c)
	}

	// 解析服务器地址
	udpAddr, err := net.ResolveUDPAddr("udp", address)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve UDP address: %w", err)
	}

	// 解析本地地址（如果指定）
	if c.localAddr != nil {
		c.conn, err = net.ListenUDP("udp", c.localAddr)
	} else {
		c.conn, err = net.ListenUDP("udp", nil)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to create UDP connection: %w", err)
	}

	c.address = udpAddr.String()
	return c, nil
}

// applyDeadlines 统一设置读写截止时间：若设置了 resilienceKey，则优先使用
// resilience 策略的 Timeout 覆盖客户端自身的读写超时，实现按下游动态调超时。
func (c *Client) applyDeadlines(conn *net.UDPConn) {
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

// WithLocalAddress 设置本地地址
func WithLocalAddress(addr string) Option {
	return func(c *Client) {
		if localAddr, err := net.ResolveUDPAddr("udp", addr); err == nil {
			c.localAddr = localAddr
		}
	}
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

// WithBufferSize 设置缓冲区大小
func WithBufferSize(size int) Option {
	return func(c *Client) {
		c.bufSize = size
	}
}

// WithResilienceKey 设置 resilience 的 serviceKey；非空时 resilience 策略的 Timeout
// 会覆盖客户端自身的读写超时，实现按下游动态调超时。
func WithResilienceKey(key string) Option {
	return func(c *Client) {
		c.resilienceKey = key
	}
}

// Send 发送数据
func (c *Client) Send(ctx context.Context, data []byte) error {
	c.connMu.RLock()
	conn := c.conn
	c.connMu.RUnlock()

	if conn == nil {
		return fmt.Errorf("connection is nil")
	}

	// 解析目标地址
	addr, err := net.ResolveUDPAddr("udp", c.address)
	if err != nil {
		return fmt.Errorf("failed to resolve address: %w", err)
	}

	// 设置超时
	c.applyDeadlines(conn)

	// 发送数据
	n, err := conn.WriteToUDP(data, addr)
	if err != nil {
		return fmt.Errorf("send failed: %w", err)
	}

	if n != len(data) {
		return fmt.Errorf("partial send: sent %d, expected %d", n, len(data))
	}

	return nil
}

// SendTo 发送到指定地址
func (c *Client) SendTo(ctx context.Context, address string, data []byte) error {
	c.connMu.RLock()
	conn := c.conn
	c.connMu.RUnlock()

	if conn == nil {
		return fmt.Errorf("connection is nil")
	}

	addr, err := net.ResolveUDPAddr("udp", address)
	if err != nil {
		return fmt.Errorf("failed to resolve address: %w", err)
	}

	c.applyDeadlines(conn)

	n, err := conn.WriteToUDP(data, addr)
	if err != nil {
		return fmt.Errorf("send failed: %w", err)
	}

	if n != len(data) {
		return fmt.Errorf("partial send: sent %d, expected %d", n, len(data))
	}

	return nil
}

// Receive 接收数据
func (c *Client) Receive(ctx context.Context) ([]byte, *net.UDPAddr, error) {
	c.connMu.RLock()
	conn := c.conn
	c.connMu.RUnlock()

	if conn == nil {
		return nil, nil, fmt.Errorf("connection is nil")
	}

	c.applyDeadlines(conn)

	data := make([]byte, c.bufSize)
	n, addr, err := conn.ReadFromUDP(data)
	if err != nil {
		return nil, nil, fmt.Errorf("receive failed: %w", err)
	}

	return data[:n], addr, nil
}

// ReceiveWithBuffer 使用指定缓冲区接收
func (c *Client) ReceiveWithBuffer(ctx context.Context, buf []byte) (int, *net.UDPAddr, error) {
	c.connMu.RLock()
	conn := c.conn
	c.connMu.RUnlock()

	if conn == nil {
		return 0, nil, fmt.Errorf("connection is nil")
	}

	c.applyDeadlines(conn)

	n, addr, err := conn.ReadFromUDP(buf)
	if err != nil {
		return 0, nil, fmt.Errorf("receive failed: %w", err)
	}

	return n, addr, nil
}

// SendAndReceive 发送并等待响应
func (c *Client) SendAndReceive(ctx context.Context, data []byte) ([]byte, error) {
	// 发送数据
	if err := c.Send(ctx, data); err != nil {
		return nil, err
	}

	// 接收响应
	resp, _, err := c.Receive(ctx)
	if err != nil {
		return nil, err
	}

	return resp, nil
}

// SendAsync 异步发送（不等待响应）
func (c *Client) SendAsync(ctx context.Context, data []byte) {
	go func() {
		_ = c.Send(ctx, data)
	}()
}

// Broadcast 广播消息
func (c *Client) Broadcast(ctx context.Context, port int, data []byte) error {
	addr := &net.UDPAddr{
		IP:   net.IPv4bcast,
		Port: port,
	}

	c.connMu.RLock()
	conn := c.conn
	c.connMu.RUnlock()

	if conn == nil {
		return fmt.Errorf("connection is nil")
	}

	c.applyDeadlines(conn)

	// 设置广播选项
	if err := conn.SetWriteBuffer(c.bufSize); err != nil {
		return fmt.Errorf("failed to set write buffer: %w", err)
	}

	n, err := conn.WriteToUDP(data, addr)
	if err != nil {
		return fmt.Errorf("broadcast failed: %w", err)
	}

	if n != len(data) {
		return fmt.Errorf("partial broadcast: sent %d, expected %d", n, len(data))
	}

	return nil
}

// Multicast 组播消息
func (c *Client) Multicast(ctx context.Context, multicastAddr string, data []byte) error {
	addr, err := net.ResolveUDPAddr("udp", multicastAddr)
	if err != nil {
		return fmt.Errorf("failed to resolve multicast address: %w", err)
	}

	c.connMu.RLock()
	conn := c.conn
	c.connMu.RUnlock()

	if conn == nil {
		return fmt.Errorf("connection is nil")
	}

	c.applyDeadlines(conn)

	n, err := conn.WriteToUDP(data, addr)
	if err != nil {
		return fmt.Errorf("multicast failed: %w", err)
	}

	if n != len(data) {
		return fmt.Errorf("partial multicast: sent %d, expected %d", n, len(data))
	}

	return nil
}

// IsConnected 检查连接状态
func (c *Client) IsConnected() bool {
	c.connMu.RLock()
	defer c.connMu.RUnlock()
	return c.conn != nil
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
func (c *Client) GetConn() *net.UDPConn {
	c.connMu.RLock()
	defer c.connMu.RUnlock()
	return c.conn
}

// LocalAddr 返回本地地址
func (c *Client) LocalAddr() net.Addr {
	c.connMu.RLock()
	defer c.connMu.RUnlock()

	if c.conn == nil {
		return nil
	}
	return c.conn.LocalAddr()
}

// ServerConn 创建服务端 UDP 连接（监听模式）
func ServerConn(address string) (*net.UDPConn, error) {
	addr, err := net.ResolveUDPAddr("udp", address)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve address: %w", err)
	}

	conn, err := net.ListenUDP("udp", addr)
	if err != nil {
		return nil, fmt.Errorf("failed to listen: %w", err)
	}

	return conn, nil
}
