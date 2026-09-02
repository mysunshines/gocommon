// Package prometheus 提供 Prometheus HTTP 查询客户端的轻量实现。
// 仅依赖标准库，支持即时查询 (query) 与区间查询 (query_range)，
// 并将结果解析为易于消费的时间序列结构，供 report-service 生成性能报告。
package prometheus

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/mysunshines/gocommon/constants"
	"github.com/mysunshines/gocommon/log"
	"github.com/mysunshines/gocommon/middleware"

	"github.com/sirupsen/logrus"
)

// Sample 表示一个时间点上的采样值。
type Sample struct {
	Timestamp time.Time // 采样时刻
	Value     float64   // 采样数值
}

// Series 表示一条时间序列（矩阵为多点，向量为单点）。
type Series struct {
	Metric  map[string]string // 标签集合（如 {instance, job}），用于区分不同序列
	Samples []Sample          // 区间查询结果（matrix），含多个时间点
	Value   *Sample           // 即时查询结果（vector），仅单点
}

// QueryResult 是查询响应的统一结构。
type QueryResult struct {
	ResultType string   // 结果类型："matrix"（区间）或 "vector"（即时）
	Series     []Series // 查询结果的时间序列列表
}

// Client 是 Prometheus HTTP API 客户端。
type Client struct {
	baseURL string       // Prometheus 基础地址，如 http://prometheus:9090
	http    *http.Client // 底层 HTTP 客户端
}

// New 创建客户端。baseURL 形如 http://prometheus:9090。
func New(baseURL string, timeout time.Duration) *Client {
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	return &Client{
		baseURL: baseURL,
		http: &http.Client{
			Timeout: timeout,
			Transport: &http.Transport{
				MaxIdleConns:        100,
				MaxIdleConnsPerHost: 100,
				IdleConnTimeout:     90 * time.Second,
			},
		},
	}
}

// Query 执行即时查询，t 为零值时使用 Prometheus 服务端当前时间。
func (c *Client) Query(ctx context.Context, promQL string, t time.Time) (*QueryResult, error) {
	q := url.Values{}
	q.Set("query", promQL)
	if !t.IsZero() {
		q.Set("time", strconv.FormatInt(t.Unix(), 10))
	}
	body, err := c.do(ctx, "/api/v1/query", q)
	if err != nil {
		return nil, err
	}
	return parseResponse(body, false)
}

// QueryRange 执行区间查询，返回 [start, end] 内按 step 间隔采样的矩阵结果。
func (c *Client) QueryRange(ctx context.Context, promQL string, start, end time.Time, step time.Duration) (*QueryResult, error) {
	if step <= 0 {
		step = time.Minute
	}
	q := url.Values{}
	q.Set("query", promQL)
	q.Set("start", strconv.FormatInt(start.Unix(), 10))
	q.Set("end", strconv.FormatInt(end.Unix(), 10))
	q.Set("step", strconv.FormatInt(int64(step.Seconds()), 10))
	body, err := c.do(ctx, "/api/v1/query_range", q)
	if err != nil {
		return nil, err
	}
	return parseResponse(body, true)
}

func (c *Client) do(ctx context.Context, path string, q url.Values) ([]byte, error) {
	u := c.baseURL + path + "?" + q.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, fmt.Errorf("prometheus: build request failed: %w", err)
	}
	traceID := middleware.GetTraceIDFromContext(ctx)
	start := time.Now()
	resp, err := c.http.Do(req)
	if err != nil {
		log.WithFields(logrus.Fields{
			constants.LogFieldTraceID: traceID,
			"url":                     u,
			"query":                   q.Get("query"),
			"duration":                time.Since(start).String(),
			"err":                     err.Error(),
		}).Errorf("[prometheus] query failed")
		return nil, fmt.Errorf("prometheus: query %q failed: %w", q.Get("query"), err)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		log.WithFields(logrus.Fields{
			constants.LogFieldTraceID: traceID,
			"url":                     u,
			"status":                  resp.StatusCode,
			"duration":                time.Since(start).String(),
			"err":                     err.Error(),
		}).Errorf("[prometheus] read response failed")
		return nil, fmt.Errorf("prometheus: read response failed: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		log.WithFields(logrus.Fields{
			constants.LogFieldTraceID: traceID,
			"url":                     u,
			"status":                  resp.StatusCode,
			"duration":                time.Since(start).String(),
			"body":                    truncate(string(data), 256),
		}).Errorf("[prometheus] unexpected status")
		return nil, fmt.Errorf("prometheus: unexpected status %d: %s", resp.StatusCode, truncate(string(data), 256))
	}
	log.WithFields(logrus.Fields{
		constants.LogFieldTraceID: traceID,
		"url":                     u,
		"query":                   q.Get("query"),
		"status":                  resp.StatusCode,
		"duration":                time.Since(start).String(),
	}).Debugf("[prometheus] query completed")
	return data, nil
}

type apiResponse struct {
	Status string `json:"status"` // 查询状态："success" / "error"
	Error  string `json:"error"`  // 失败时的错误信息
	Data   struct {
		ResultType string          `json:"resultType"` // 结果类型
		Result     json.RawMessage `json:"result"`     // 原始结果（matrix/vector），延迟解析
	} `json:"data"`
}

type rawMatrix struct {
	Metric map[string]string `json:"metric"` // 标签集合
	Values [][2]interface{}  `json:"values"` // [时间戳, 字符串数值] 序列
}

type rawVector struct {
	Metric map[string]string `json:"metric"` // 标签集合
	Value  [2]interface{}    `json:"value"`  // [时间戳, 字符串数值]，单点
}

func parseResponse(body []byte, isRange bool) (*QueryResult, error) {
	var ar apiResponse
	if err := json.Unmarshal(body, &ar); err != nil {
		return nil, fmt.Errorf("prometheus: parse response failed: %w", err)
	}
	if ar.Status != "success" {
		return nil, fmt.Errorf("prometheus: query error: %s", ar.Error)
	}
	res := &QueryResult{ResultType: ar.Data.ResultType}
	if isRange {
		var matrix []rawMatrix
		if err := json.Unmarshal(ar.Data.Result, &matrix); err != nil {
			return nil, fmt.Errorf("prometheus: parse matrix failed: %w", err)
		}
		for _, item := range matrix {
			s := Series{Metric: item.Metric}
			for _, v := range item.Values {
				ts, val, ok := parseSample(v)
				if !ok {
					continue
				}
				s.Samples = append(s.Samples, Sample{Timestamp: ts, Value: val})
			}
			if len(s.Samples) > 0 {
				res.Series = append(res.Series, s)
			}
		}
	} else {
		var vector []rawVector
		if err := json.Unmarshal(ar.Data.Result, &vector); err != nil {
			return nil, fmt.Errorf("prometheus: parse vector failed: %w", err)
		}
		for _, item := range vector {
			s := Series{Metric: item.Metric}
			if ts, val, ok := parseSample(item.Value); ok {
				s.Value = &Sample{Timestamp: ts, Value: val}
			}
			res.Series = append(res.Series, s)
		}
	}
	return res, nil
}

func parseSample(v [2]interface{}) (time.Time, float64, bool) {
	ts, ok := v[0].(float64)
	if !ok {
		return time.Time{}, 0, false
	}
	str, ok := v[1].(string)
	if !ok {
		return time.Time{}, 0, false
	}
	f, err := strconv.ParseFloat(str, 64)
	if err != nil {
		return time.Time{}, 0, false
	}
	return time.Unix(int64(ts), 0).UTC(), f, true
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
