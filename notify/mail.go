// Package notify 提供基于标准库 net/smtp 的邮件发送能力，零外部依赖。
// 支持 HTML 正文与内联图片（通过 Content-ID 引用），用于发送富文本性能报告。
package notify

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/base64"
	"fmt"
	"mime"
	"net"
	"net/smtp"
	"strconv"
	"strings"
	"time"

	"github.com/mysunshines/gocommon/constants"
	"github.com/mysunshines/gocommon/log"
	"github.com/mysunshines/gocommon/middleware"

	"github.com/sirupsen/logrus"
)

// Image 表示一封邮件中的内联图片，HTML 中通过 <img src="cid:<CID>"> 引用。
type Image struct {
	CID  string // 内容 ID，需与 HTML 中的 cid: 对应
	Name string // 文件名（用于 Content-Disposition）
	Data []byte // PNG 图片二进制
}

// Message 表示一封待发送邮件。
type Message struct {
	From     string   // 发件地址（为空则用 Config.From）
	FromName string   // 发件人显示名
	To       []string // 收件人地址列表
	Cc       []string // 抄送地址列表
	Subject  string   // 邮件主题
	TextBody string   // 纯文本正文
	HTMLBody string   // HTML 正文
	Images   []Image  // 内联图片列表（HTML 中以 cid 引用）
}

// Config 邮件服务器配置。
type Config struct {
	Host     string
	Port     int
	Username string
	Password string
	From     string
	FromName string
	// UseTLS=true 表示隐式 TLS（通常用于 465 端口）；
	// false 表示明文连接并由客户端尝试 STARTTLS（通常用于 587 端口）。
	UseTLS bool
}

const (
	boundaryRelated    = "reportRELATEDboundary"
	boundaryAlternative = "reportALTERNATIVEboundary"
)

// Send 发送邮件。成功返回 nil。ctx 用于提取 traceID 串联日志。
func Send(ctx context.Context, cfg Config, msg Message) error {
	from := firstNonEmpty(msg.From, cfg.From)
	if from == "" {
		return fmt.Errorf("notify: missing sender address")
	}
	if len(msg.To) == 0 {
		return fmt.Errorf("notify: missing recipients")
	}

	var buf bytes.Buffer
	buf.WriteString("From: " + formatAddr(firstNonEmpty(msg.FromName, cfg.FromName), from) + "\r\n")
	buf.WriteString("To: " + strings.Join(msg.To, ", ") + "\r\n")
	if len(msg.Cc) > 0 {
		buf.WriteString("Cc: " + strings.Join(msg.Cc, ", ") + "\r\n")
	}
	buf.WriteString("Subject: " + encodeSubject(msg.Subject) + "\r\n")
	buf.WriteString("Date: " + time.Now().Format(time.RFC1123Z) + "\r\n")
	buf.WriteString("MIME-Version: 1.0\r\n")

	if len(msg.Images) == 0 {
		buf.WriteString("Content-Type: multipart/alternative; boundary=" + boundaryAlternative + "\r\n\r\n")
		writeAlternative(&buf, msg)
	} else {
		buf.WriteString("Content-Type: multipart/related; boundary=" + boundaryRelated + "\r\n\r\n")
		// 内联的 alternative 部分
		buf.WriteString("--" + boundaryRelated + "\r\n")
		buf.WriteString("Content-Type: multipart/alternative; boundary=" + boundaryAlternative + "\r\n\r\n")
		writeAlternative(&buf, msg)
		buf.WriteString("--" + boundaryAlternative + "--\r\n")

		for _, img := range msg.Images {
			buf.WriteString("--" + boundaryRelated + "\r\n")
			buf.WriteString(fmt.Sprintf("Content-Type: image/png; name=\"%s\"\r\n", img.Name))
			buf.WriteString("Content-Transfer-Encoding: base64\r\n")
			buf.WriteString(fmt.Sprintf("Content-ID: <%s>\r\n", img.CID))
			buf.WriteString(fmt.Sprintf("Content-Disposition: inline; filename=\"%s\"\r\n", img.Name))
			buf.WriteString("\r\n")
			enc := base64.StdEncoding.EncodeToString(img.Data)
			for len(enc) > 76 {
				buf.WriteString(enc[:76] + "\r\n")
				enc = enc[76:]
			}
			buf.WriteString(enc + "\r\n")
		}
		buf.WriteString("--" + boundaryRelated + "--\r\n")
	}

	recipients := append(append([]string{}, msg.To...), msg.Cc...)
	traceID := middleware.GetTraceIDFromContext(ctx)
	log.WithFields(logrus.Fields{
		constants.LogFieldTraceID: traceID,
		"from":                    from,
		"to":                      strings.Join(recipients, ","),
		"subject":                 msg.Subject,
		"images":                  len(msg.Images),
	}).Infof("[notify] sending mail")
	err := sendSMTP(ctx, cfg, from, recipients, buf.Bytes())
	if err != nil {
		log.WithFields(logrus.Fields{
			constants.LogFieldTraceID: traceID,
			"from":                    from,
			"to":                      strings.Join(recipients, ","),
			"subject":                 msg.Subject,
			"err":                     err.Error(),
		}).Errorf("[notify] send mail failed")
		return err
	}
	log.WithFields(logrus.Fields{
		constants.LogFieldTraceID: traceID,
		"from":                    from,
		"to":                      strings.Join(recipients, ","),
		"subject":                 msg.Subject,
	}).Infof("[notify] mail sent")
	return nil
}

func writeAlternative(buf *bytes.Buffer, msg Message) {
	if msg.TextBody != "" {
		buf.WriteString("--" + boundaryAlternative + "\r\n")
		buf.WriteString("Content-Type: text/plain; charset=UTF-8\r\n\r\n")
		buf.WriteString(msg.TextBody + "\r\n")
	}
	if msg.HTMLBody != "" {
		buf.WriteString("--" + boundaryAlternative + "\r\n")
		buf.WriteString("Content-Type: text/html; charset=UTF-8\r\n\r\n")
		buf.WriteString(msg.HTMLBody + "\r\n")
	}
}

// smtpSessionTimeout SMTP 会话整体超时：覆盖握手/鉴权/收件/正文写入的读写。
// net/smtp 自身不提供命令级超时，若服务器不响应会无限阻塞，因此对整个会话
// 设置 deadline（普通发送远小于该值，仅为防挂起兜底）。
const smtpSessionTimeout = 30 * time.Second

func sendSMTP(ctx context.Context, cfg Config, from string, to []string, data []byte) error {
	traceID := middleware.GetTraceIDFromContext(ctx)
	addr := net.JoinHostPort(cfg.Host, strconv.Itoa(cfg.Port))
	var auth smtp.Auth
	if cfg.Username != "" {
		auth = smtp.PlainAuth("", cfg.Username, cfg.Password, cfg.Host)
	}

	// 统一带超时建连：net.DialTimeout / tls.DialWithDialer 均在拨号阶段设超时，
	// 避免 SMTP 服务器不可达时挂起（历史 tls.Dial / smtp.SendMail 均无超时参数）。
	dialer := &net.Dialer{Timeout: constants.DefaultDialTimeout * time.Second}
	var conn net.Conn
	var err error
	if cfg.UseTLS {
		conn, err = tls.DialWithDialer(dialer, "tcp", addr, &tls.Config{ServerName: cfg.Host})
		if err != nil {
			log.WithFields(logrus.Fields{
				constants.LogFieldTraceID: traceID,
				"addr":                    addr,
				"err":                     err.Error(),
			}).Errorf("[notify] tls dial failed")
			return fmt.Errorf("notify: tls dial %s failed: %w", addr, err)
		}
	} else {
		conn, err = dialer.Dial("tcp", addr)
		if err != nil {
			log.WithFields(logrus.Fields{
				constants.LogFieldTraceID: traceID,
				"addr":                    addr,
				"err":                     err.Error(),
			}).Errorf("[notify] smtp dial failed")
			return fmt.Errorf("notify: smtp dial %s failed: %w", addr, err)
		}
	}
	defer conn.Close()
	// 会话级 deadline：任何卡住的 SMTP 交互都会在此超时返回错误
	_ = conn.SetDeadline(time.Now().Add(smtpSessionTimeout))

	c, err := smtp.NewClient(conn, cfg.Host)
	if err != nil {
		log.WithFields(logrus.Fields{
			constants.LogFieldTraceID: traceID,
			"addr":                    addr,
			"err":                     err.Error(),
		}).Errorf("[notify] smtp client failed")
		return fmt.Errorf("notify: smtp client failed: %w", err)
	}
	defer c.Quit()
	if !cfg.UseTLS {
		// 与原 smtp.SendMail 行为一致：服务器支持时自动升级 STARTTLS
		if ok, _ := c.Extension("STARTTLS"); ok {
			if err := c.StartTLS(&tls.Config{ServerName: cfg.Host}); err != nil {
				log.WithFields(logrus.Fields{
					constants.LogFieldTraceID: traceID,
					"addr":                    addr,
					"err":                     err.Error(),
				}).Errorf("[notify] smtp STARTTLS failed")
				return fmt.Errorf("notify: smtp STARTTLS failed: %w", err)
			}
		}
	}
	if auth != nil {
		if err := c.Auth(auth); err != nil {
			log.WithFields(logrus.Fields{
				constants.LogFieldTraceID: traceID,
				"addr":                    addr,
				"err":                     err.Error(),
			}).Errorf("[notify] smtp auth failed")
			return fmt.Errorf("notify: smtp auth failed: %w", err)
		}
	}
	if err := c.Mail(from); err != nil {
		log.WithFields(logrus.Fields{
			constants.LogFieldTraceID: traceID,
			"from":                    from,
			"err":                     err.Error(),
		}).Errorf("[notify] smtp MAIL FROM failed")
		return fmt.Errorf("notify: smtp MAIL FROM failed: %w", err)
	}
	for _, rcpt := range to {
		if err := c.Rcpt(rcpt); err != nil {
			log.WithFields(logrus.Fields{
				constants.LogFieldTraceID: traceID,
				"rcpt":                    rcpt,
				"err":                     err.Error(),
			}).Errorf("[notify] smtp RCPT failed")
			return fmt.Errorf("notify: smtp RCPT %s failed: %w", rcpt, err)
		}
	}
	w, err := c.Data()
	if err != nil {
		log.WithFields(logrus.Fields{
			constants.LogFieldTraceID: traceID,
			"err":                     err.Error(),
		}).Errorf("[notify] smtp DATA failed")
		return fmt.Errorf("notify: smtp DATA failed: %w", err)
	}
	if _, err := w.Write(data); err != nil {
		log.WithFields(logrus.Fields{
			constants.LogFieldTraceID: traceID,
			"err":                     err.Error(),
		}).Errorf("[notify] write mail body failed")
		return fmt.Errorf("notify: write mail body failed: %w", err)
	}
	if err := w.Close(); err != nil {
		log.WithFields(logrus.Fields{
			constants.LogFieldTraceID: traceID,
			"err":                     err.Error(),
		}).Errorf("[notify] close mail data failed")
		return err
	}
	return nil
}

func formatAddr(name, addr string) string {
	if name == "" {
		return addr
	}
	return fmt.Sprintf("%s <%s>", name, addr)
}

func encodeSubject(s string) string {
	if isASCII(s) {
		return s
	}
	return mime.QEncoding.Encode("UTF-8", s)
}

func isASCII(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] > 127 {
			return false
		}
	}
	return true
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}
