// Package notify 提供基于标准库 net/smtp 的邮件发送能力，零外部依赖。
// 支持 HTML 正文与内联图片（通过 Content-ID 引用），用于发送富文本性能报告。
package notify

import (
	"bytes"
	"crypto/tls"
	"encoding/base64"
	"fmt"
	"mime"
	"net/smtp"
	"strings"
	"time"
)

// Image 表示一封邮件中的内联图片，HTML 中通过 <img src="cid:<CID>"> 引用。
type Image struct {
	CID  string // 内容 ID，需与 HTML 中的 cid: 对应
	Name string // 文件名（用于 Content-Disposition）
	Data []byte // PNG 图片二进制
}

// Message 表示一封待发送邮件。
type Message struct {
	From     string
	FromName string
	To       []string
	Cc       []string
	Subject  string
	TextBody string
	HTMLBody string
	Images   []Image
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

// Send 发送邮件。成功返回 nil。
func Send(cfg Config, msg Message) error {
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
	return sendSMTP(cfg, from, recipients, buf.Bytes())
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

func sendSMTP(cfg Config, from string, to []string, data []byte) error {
	addr := fmt.Sprintf("%s:%d", cfg.Host, cfg.Port)
	var auth smtp.Auth
	if cfg.Username != "" {
		auth = smtp.PlainAuth("", cfg.Username, cfg.Password, cfg.Host)
	}

	if cfg.UseTLS {
		conn, err := tls.Dial("tcp", addr, &tls.Config{ServerName: cfg.Host})
		if err != nil {
			return fmt.Errorf("notify: tls dial %s failed: %w", addr, err)
		}
		defer conn.Close()
		c, err := smtp.NewClient(conn, cfg.Host)
		if err != nil {
			return fmt.Errorf("notify: smtp client failed: %w", err)
		}
		defer c.Quit()
		if auth != nil {
			if err := c.Auth(auth); err != nil {
				return fmt.Errorf("notify: smtp auth failed: %w", err)
			}
		}
		if err := c.Mail(from); err != nil {
			return fmt.Errorf("notify: smtp MAIL FROM failed: %w", err)
		}
		for _, rcpt := range to {
			if err := c.Rcpt(rcpt); err != nil {
				return fmt.Errorf("notify: smtp RCPT %s failed: %w", rcpt, err)
			}
		}
		w, err := c.Data()
		if err != nil {
			return fmt.Errorf("notify: smtp DATA failed: %w", err)
		}
		if _, err := w.Write(data); err != nil {
			return fmt.Errorf("notify: write mail body failed: %w", err)
		}
		return w.Close()
	}

	return smtp.SendMail(addr, auth, from, to, data)
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
