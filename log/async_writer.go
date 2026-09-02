package log

import (
	"io"
	"sync"
	"sync/atomic"
)

// AsyncWriter 是一个有界缓冲的异步日志写入器。
//
// 设计目标（防止日志 I/O 阻塞拖垮主流程）：
//   - 调用方（请求 goroutine）仅把日志字节投递到内存 channel，立即返回，绝不阻塞业务逻辑；
//   - 独立的后台 goroutine 从 channel 取出并写入底层 writer（stdout / 文件）；
//   - channel 满时【直接丢弃】并累计丢弃计数，而不是阻塞调用方——
//     宁可丢失少量日志，也绝不让日志写入变慢把整个服务拖垮（行业通行做法，如 zap 异步亦如此）；
//   - 底层写入目标可热替换（SetDst），用于日志按天轮转时无缝切换文件；
//   - 提供 Close() 做优雅 drain：停止接收新日志后，把 channel 中残留的日志尽量写完再返回。
type AsyncWriter struct {
	ch        chan []byte
	dropCount int64 // 原子计数：因 channel 满而丢弃的日志条数
	dst       atomic.Value // 存当前 io.Writer，支持热替换（日志轮转）
	wg        sync.WaitGroup
	once      sync.Once
	closed    int32 // 1 表示已关闭，写入直接丢弃
}

// NewAsyncWriter 创建异步写入器。
// bufferSize 为 channel 缓冲大小（建议 1024~8192），dst 为真正的底层写入目标。
func NewAsyncWriter(dst io.Writer, bufferSize int) *AsyncWriter {
	if bufferSize <= 0 {
		bufferSize = 4096
	}
	w := &AsyncWriter{
		ch: make(chan []byte, bufferSize),
	}
	w.dst.Store(dst)
	w.wg.Add(1)
	go func() {
		defer w.wg.Done()
		for b := range w.ch {
			// 即使底层写入慢/出错，也只在当前条目上等待，不影响投递端。
			if d, ok := w.dst.Load().(io.Writer); ok && d != nil {
				_, _ = d.Write(b)
			}
		}
	}()
	return w
}

// SetDst 热替换底层写入目标，供日志轮转时切换文件而不中断异步管道。
func (w *AsyncWriter) SetDst(dst io.Writer) {
	w.dst.Store(dst)
}

// Write 实现 io.Writer：把 p 拷贝后投递到 channel 并立即返回。
// channel 满或已关闭时直接丢弃，保证调用方永不阻塞。
func (w *AsyncWriter) Write(p []byte) (int, error) {
	if atomic.LoadInt32(&w.closed) == 1 {
		return len(p), nil
	}
	// 拷贝，避免底层 buffer 被复用导致内容错乱。
	buf := make([]byte, len(p))
	copy(buf, p)
	select {
	case w.ch <- buf:
		return len(p), nil
	default:
		// 满则丢弃，绝不阻塞。
		atomic.AddInt64(&w.dropCount, 1)
		return len(p), nil
	}
}

// Dropped 返回累计被丢弃的日志条数（可用于监控告警）。
func (w *AsyncWriter) Dropped() int64 {
	return atomic.LoadInt64(&w.dropCount)
}

// Close 优雅关闭：标记 closed，关闭 channel，等待后台 goroutine 把残留日志写完。
// 多次调用安全。Close 之后该 writer 不可再用于写入。
func (w *AsyncWriter) Close() error {
	w.once.Do(func() {
		atomic.StoreInt32(&w.closed, 1)
		close(w.ch)
	})
	w.wg.Wait()
	return nil
}
