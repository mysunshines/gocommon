// Package configcenter 提供基于 Consul KV 的轻量级配置中心能力，使业务配置
// 可在后台（Consul Web UI 或自研管理页）直接修改并即时生效，无需发版重启。
//
// 设计目标（与 gocommon/consul 保持一致）：
//   - 不依赖第三方 consul SDK，仅用标准库 + gocommon/log 调用 Consul HTTP KV API。
//   - 启动时从 Consul KV 拉取 YAML；通过 Consul 的 blocking query（?index=）长轮询
//     监听变更，命中后原子刷新内存中的配置，并触发可选的 OnUpdate 回调。
//   - Consul 不可达或 KV key 不存在时仅返回 error（不致命），便于本地开发/无 Consul
//     环境降级到 config_xxx.yaml 默认值。
//
// 适用场景：各微服务启动时调用 Load 拉取热更配置，再调用 Watch 在后台监听变更。
// 仅将"确实需要在线上即时调整"的配置（如限流阈值、日志级别、JWT 时效、业务开关）
// 纳入热更范围；数据库连接、Redis、Consul 地址等基础设施配置仍走 yaml + 环境变量，
// 因为它们往往需要重建连接或重启才安全。
package configcenter

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/mysunshines/gocommon/log"
)

// DefaultKVBasePath 是热更配置在 Consul KV 中的根路径。
// 完整 key 形如：config/<service>/<env>，例如 config/article-service/production。
const DefaultKVBasePath = "config"

// ErrNotFound 表示 Consul KV 中不存在对应的配置 key（尚未在后台写入）。
// 调用方应据此降级到 yaml 默认值，而不是当作致命错误。
var ErrNotFound = fmt.Errorf("configcenter: key not found in consul kv")

// Key 拼接某个服务在某个环境下的完整 KV key。
//
// 示例：
//
//	Key("article-service", "production") // => "config/article-service/production"
func Key(service, env string) string {
	return fmt.Sprintf("%s/%s/%s", DefaultKVBasePath, service, env)
}

// Client 封装对 Consul KV 的读写与监听。
// 内部通过 atomic.Value 持有最新反序列化结果，供并发读取无锁访问。
type Client struct {
	address string        // Consul Agent 地址，形如 host:port（如 consul:8500）
	http    *http.Client  // 复用的 HTTP 客户端
	done    chan struct{} // 关闭后 Watch 停止

	mu    sync.RWMutex
	index uint64 // 最近一次 KV 的 ModifyIndex，用于 blocking query
}

// New 创建一个指向指定 Consul 地址的 Client。
func New(consulAddress string) *Client {
	return &Client{
		address: consulAddress,
		http:    &http.Client{Timeout: 10 * time.Second},
		done:    make(chan struct{}),
	}
}

// Stop 停止该 Client 上的 Watch 长轮询（幂等）。
func (c *Client) Stop() {
	select {
	case <-c.done:
		// 已关闭
	default:
		close(c.done)
	}
}

// stopped 报告是否已收到停止信号。
func (c *Client) stopped() bool {
	select {
	case <-c.done:
		return true
	default:
		return false
	}
}

// kvURL 返回带 blocking query 参数的 KV 读取 URL。
// wait 为长轮询最长等待时间（如 "10m"）；index 为上次已知的 ModifyIndex，
// 传 0 表示首次读取。
func (c *Client) kvURL(key string, wait string, index uint64) string {
	u := fmt.Sprintf("http://%s/v1/kv/%s", c.address, key)
	if wait != "" || index > 0 {
		sep := "?"
		if wait != "" {
			u += sep + "wait=" + wait
			sep = "&"
		}
		if index > 0 {
			u += sep + fmt.Sprintf("index=%d", index)
		}
	}
	return u
}

// kvPutURL 返回 KV 写入 URL（供管理后台/测试使用）。
func (c *Client) kvPutURL(key string) string {
	return fmt.Sprintf("http://%s/v1/kv/%s", c.address, key)
}

// get 从 Consul KV 读取 key，返回解码后的 value 与最新 ModifyIndex。
// 当 key 不存在时返回 ErrNotFound。
func (c *Client) get(key string) ([]byte, uint64, error) {
	resp, err := c.http.Get(c.kvURL(key, "", 0))
	if err != nil {
		return nil, 0, fmt.Errorf("configcenter: get %s failed: %w", key, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return nil, 0, ErrNotFound
	}
	if resp.StatusCode != http.StatusOK {
		return nil, 0, fmt.Errorf("configcenter: get %s returned status %d", key, resp.StatusCode)
	}

	var entries []struct {
		Value       string `json:"Value"` // base64 编码的值
		ModifyIndex uint64 `json:"ModifyIndex"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&entries); err != nil {
		return nil, 0, fmt.Errorf("configcenter: decode %s response: %w", key, err)
	}
	if len(entries) == 0 {
		return nil, 0, ErrNotFound
	}

	raw, err := decodeValue(entries[0].Value)
	if err != nil {
		return nil, 0, fmt.Errorf("configcenter: base64 decode %s: %w", key, err)
	}
	return raw, entries[0].ModifyIndex, nil
}

// Load 从 Consul KV 拉取 key 对应的 YAML/JSON 并反序列化到 out（指针）。
// 当 key 不存在时返回 ErrNotFound，调用方应降级到本地默认值。
func (c *Client) Load(key string, out interface{}) error {
	raw, index, err := c.get(key)
	if err != nil {
		return err
	}
	c.mu.Lock()
	c.index = index
	c.mu.Unlock()

	if err := unmarshalAuto(raw, out); err != nil {
		return fmt.Errorf("configcenter: unmarshal %s: %w", key, err)
	}
	log.Infof("configcenter: loaded hot config from kv %s (index=%d)", key, index)
	return nil
}

// Watch 长轮询监听 key 的变更：每当 Consul KV 中该 key 被修改，
// 就重新拉取并反序列化到 out，再原子更新内部快照并调用 onUpdate（可为 nil）。
//
// 语义说明：
//   - 本轮 Watch 的第一次拉取（内部 index 为 0）仅用于初始化，不触发 onUpdate；
//     真正的初始化回写由调用方（ServiceConfig.Watch）通过 Load 完成。
//   - 仅当 KV 的 ModifyIndex 真正推进（即发生变更）时才重新拉取并回调 onUpdate。
//
// 阻塞式，应在独立 goroutine 中调用（如 go client.Watch(...)）。
// 仅当收到 Stop 信号或 Consul 持续不可达时才会退出；偶发网络抖动会被内部重试吸收。
func (c *Client) Watch(key string, out interface{}, onUpdate func()) error {
	// 复用底层 *http.Client 的 transport，使 Stop 时能通过关闭连接中断阻塞查询。
	reqCtx := c.done
	for {
		if c.stopped() {
			return nil
		}
		idx := c.currentIndex()
		req, err := http.NewRequest(http.MethodGet, c.kvURL(key, "10m", idx), nil)
		if err != nil {
			log.Warnf("configcenter: watch %s build request failed: %v", key, err)
			time.Sleep(2 * time.Second)
			continue
		}
		// 将 done 作为请求上下文，Stop 时可中断正在进行的阻塞查询。
		req.Cancel = reqCtx

		resp, err := c.http.Do(req)
		if err != nil {
			if c.stopped() {
				return nil
			}
			log.Warnf("configcenter: watch %s request failed, retry: %v", key, err)
			time.Sleep(2 * time.Second)
			continue
		}

		// blocking query 返回时，Header 中的 X-Consul-Index 即为本次已知的最新 index。
		newIdx := parseConsulIndex(resp.Header.Get("X-Consul-Index"))
		status := resp.StatusCode
		resp.Body.Close()

		if c.stopped() {
			return nil
		}

		if status == http.StatusNotFound {
			// key 可能被删除，保持本地上次值，等待其重新出现。
			log.Warnf("configcenter: watch %s key gone, keep last value", key)
			c.mu.Lock()
			c.index = newIdx
			c.mu.Unlock()
			time.Sleep(2 * time.Second)
			continue
		}
		if status != http.StatusOK {
			log.Warnf("configcenter: watch %s returned status %d, retry", key, status)
			time.Sleep(2 * time.Second)
			continue
		}

		// 仅当 index 发生推进（包括本轮首次 idx==0 的初始化拉取）才 reload。
		if newIdx == idx {
			// 无变更（含阻塞查询超时）。真实 Consul 会通过 ?wait= 在服务端阻塞，
			// 不会空转；但在非阻塞/异常场景下为防 CPU 与连接空转，做短暂退避。
			time.Sleep(time.Second)
			continue
		}

		raw, mi, lerr := c.get(key)
		if lerr != nil {
			log.Warnf("configcenter: watch %s reload failed: %v", key, lerr)
			time.Sleep(2 * time.Second)
			continue
		}
		if err := unmarshalAuto(raw, out); err != nil {
			log.Errorf("configcenter: watch %s unmarshal failed: %v", key, err)
			time.Sleep(2 * time.Second)
			continue
		}
		c.mu.Lock()
		c.index = mi
		c.mu.Unlock()

		// 本轮首次（idx==0）属初始化拉取，不通知 onUpdate；其余视为真实变更。
		if idx != 0 && onUpdate != nil {
			onUpdate()
		}
	}
}

// currentIndex 返回内部记录的当前 ModifyIndex（线程安全）。
func (c *Client) currentIndex() uint64 {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.index
}

// Put 向 Consul KV 写入 key 的 YAML/JSON 值（供管理后台或测试调用）。
func (c *Client) Put(key string, value []byte) error {
	req, err := http.NewRequest(http.MethodPut, c.kvPutURL(key), bytesReader(value))
	if err != nil {
		return err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("configcenter: put %s failed: %w", key, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("configcenter: put %s returned status %d", key, resp.StatusCode)
	}
	return nil
}
