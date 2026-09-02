// Package minio 提供对象存储（MinIO / S3 兼容）的轻量封装，
// 供网关等需要直接落地文件的组件复用，避免每个服务各自落本地盘。
//
// 设计原则：
//   - 仅依赖官方 minio-go SDK，不绑定具体业务；
//   - 初始化时连接 MinIO 并校验 bucket 存在（不存在可自动创建并设为公共读）；
//   - 对外只暴露 Upload（存文件并返回可访问 URL）与 GetPublicURL（拼接公共读 URL）；
//   - bucket 设为公共读，文章封面等公开资源可直接被前端通过稳定 URL 加载。
package minio

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"time"

	"github.com/mysunshines/gocommon/log"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

// Config MinIO 连接配置。
type Config struct {
	Endpoint        string // MinIO API 地址，如 "minio:9000" 或 "127.0.0.1:9000"
	AccessKeyID     string
	SecretAccessKey string
	Bucket          string // 存储桶名，如 "blog"
	UseSSL          bool   // 是否使用 HTTPS（内网一般为 false）
	// PublicBaseURL 文件对外可访问的基础 URL（不含末尾斜杠），
	// 如 "http://127.0.0.1:9000" 或生产环境 "https://cdn.example.com"。
	// 返回给前端的 URL = PublicBaseURL + "/" + bucket + "/" + objectKey。
	PublicBaseURL string
	// AutoCreateBucket 为 true 时，初始化会自动创建不存在的 bucket（公共读）。
	AutoCreateBucket bool
}

// Client MinIO 客户端封装。
type Client struct {
	core   *minio.Client
	cfg    Config
	bucket string
}

// New 创建并初始化 MinIO 客户端。
// 当 cfg.AutoCreateBucket 为 true 且 bucket 不存在时自动创建并设为公共读。
func New(cfg Config) (*Client, error) {
	core, err := minio.New(cfg.Endpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(cfg.AccessKeyID, cfg.SecretAccessKey, ""),
		Secure: cfg.UseSSL,
	})
	if err != nil {
		return nil, fmt.Errorf("minio: new client: %w", err)
	}

	c := &Client{core: core, cfg: cfg, bucket: cfg.Bucket}

	if cfg.AutoCreateBucket {
		ctx := context.Background()
		exists, err := core.BucketExists(ctx, cfg.Bucket)
		if err != nil {
			return nil, fmt.Errorf("minio: check bucket %q: %w", cfg.Bucket, err)
		}
		if !exists {
			if err := core.MakeBucket(ctx, cfg.Bucket, minio.MakeBucketOptions{}); err != nil {
				return nil, fmt.Errorf("minio: make bucket %q: %w", cfg.Bucket, err)
			}
			// 设为公共读（下载），使封面等文件可被前端直接访问。
			policy := fmt.Sprintf(`{"Version":"2012-10-17","Statement":[{"Effect":"Allow","Principal":{"AWS":["*"]},"Action":["s3:GetObject"],"Resource":["arn:aws:s3:::%s/*"]}]}`, cfg.Bucket)
			if err := core.SetBucketPolicy(ctx, cfg.Bucket, policy); err != nil {
				log.Warnf("minio: set public-read policy on %q failed: %v", cfg.Bucket, err)
			}
		}
	}

	return c, nil
}

// Upload 将内容以上传对象形式存入 bucket，返回可公开访问的 URL。
// objectKey 为空时自动生成随机 key（防覆盖/目录穿越）；contentType 用于设置对象 MIME。
func (c *Client) Upload(objectKey, contentType string, data []byte) (string, error) {
	if objectKey == "" {
		objectKey = randKey(24)
	}
	_, err := c.core.PutObject(context.Background(), c.bucket, objectKey, bytes.NewReader(data), int64(len(data)),
		minio.PutObjectOptions{ContentType: contentType})
	if err != nil {
		return "", fmt.Errorf("minio: put object %q: %w", objectKey, err)
	}
	return c.GetPublicURL(objectKey), nil
}

// GetPublicURL 拼接对象的公共读访问 URL（稳定、可直接被 <img> 加载）。
func (c *Client) GetPublicURL(objectKey string) string {
	base := c.cfg.PublicBaseURL
	if base == "" {
		base = "http://" + c.cfg.Endpoint // 兜底：仅内网可达
	}
	return fmt.Sprintf("%s/%s/%s", trimTrailingSlash(base), c.bucket, objectKey)
}

// randKey 返回 n 字节随机数的十六进制字符串，用作对象 key。
func randKey(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return fmt.Sprintf("obj-%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(b)
}

func trimTrailingSlash(s string) string {
	for len(s) > 0 && s[len(s)-1] == '/' {
		s = s[:len(s)-1]
	}
	return s
}
