package udpclient

import (
	"context"
	"fmt"
	"net"
	"sync"
	"time"

	"github.com/mysunshines/gocommon/constants"
)

// Client UDP 客户端
type Client struct {
	address      string
	localAddr    *net.UDPAddr
	conn         *net.UDPConn
	connMu       sync.RWMutex
	readTimeout  time.Duration
	writeTimeout time.Duration
	bufSize      int
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
	if c.writeTimeout > 0 {
		conn.SetWriteDeadline(time.Now().Add(c.writeTimeout))
	}

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

	if c.writeTimeout > 0 {
		conn.SetWriteDeadline(time.Now().Add(c.writeTimeout))
	}

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

	if c.readTimeout > 0 {
		conn.SetReadDeadline(time.Now().Add(c.readTimeout))
	}

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

	if c.readTimeout > 0 {
		conn.SetReadDeadline(time.Now().Add(c.readTimeout))
	}

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

	if c.writeTimeout > 0 {
		conn.SetWriteDeadline(time.Now().Add(c.writeTimeout))
	}

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

	if c.writeTimeout > 0 {
		conn.SetWriteDeadline(time.Now().Add(c.writeTimeout))
	}

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
