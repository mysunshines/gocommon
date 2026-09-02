// Package cron 提供通用的系统 crontab 管理功能
// 通过操作系统的 crontab 实现进程外定时提醒
package cron

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/mysunshines/gocommon/constants"
	"github.com/mysunshines/gocommon/log"
)

// SystemCronManager 管理系统 crontab，实现进程外定时提醒
//
// 原理：
//  1. 注册一条 crontab 条目（带标记）
//  2. 修改/删除 → 更新或移除对应条目
//  3. Go 进程停止后，系统 cron daemon 仍会按时触发
//
// crontab 条目格式：
//
//	mm HH DD MM * /path/to/script "消息" "标题" # tag:type:id
type SystemCronManager struct {
	notifyScript string // 通知脚本的绝对路径
	tagPrefix    string // crontab 条目标记前缀
}

// NewSystemCronManager 创建系统 crontab 管理器
// storageRoot: 存储根目录，用于存放通知脚本
// tagPrefix: crontab 条目标记前缀，用于标识和管理条目
func NewSystemCronManager(storageRoot, tagPrefix string) *SystemCronManager {
	scriptDir := filepath.Join(storageRoot, "scripts")
	os.MkdirAll(scriptDir, constants.FilePermDir)

	scriptPath := filepath.Join(scriptDir, "notify.sh")
	if err := writeNotifyScript(scriptPath); err != nil {
		log.Infof("[SystemCron] 通知脚本写入失败: %v", err)
	}
	os.Chmod(scriptPath, constants.FilePermDir)

	// crontab 必须使用绝对路径
	absPath, _ := filepath.Abs(scriptPath)

	return &SystemCronManager{
		notifyScript: absPath,
		tagPrefix:    tagPrefix,
	}
}

// AddEntry 添加一条 crontab 条目
// fireAt: 触发时间
// title: 标题
// body: 消息内容
// tag: 条目标记（用于后续更新/删除）
func (m *SystemCronManager) AddEntry(fireAt time.Time, title, body, tag string) error {
	// 只在已过期时跳过（crontab 粒度为分钟级，不能用过大缓冲）
	if fireAt.Before(time.Now()) {
		return nil
	}

	fullTag := m.tagPrefix + tag

	// 1. 先删除同 tag 的旧条目（幂等）
	_ = m.RemoveEntry(tag)

	// 2. 读取现有 crontab
	existing, _ := m.readCrontab()

	// 3. 构造新条目
	entry := fmt.Sprintf("%d %d %d %d * %s %q %q %s",
		fireAt.Minute(),
		fireAt.Hour(),
		fireAt.Day(),
		int(fireAt.Month()),
		m.notifyScript,
		body,
		title,
		fullTag,
	)

	// 4. 写入
	if err := m.writeCrontab(append(existing, entry)); err != nil {
		return fmt.Errorf("写入 crontab 失败: %w", err)
	}

	log.Infof("[SystemCron] + %s @ %s", tag, fireAt.Format("01-02 15:04"))
	return nil
}

// RemoveEntry 移除指定 tag 的 crontab 条目
func (m *SystemCronManager) RemoveEntry(tag string) error {
	fullTag := m.tagPrefix + tag
	existing, err := m.readCrontab()
	if err != nil {
		return err
	}

	filtered := make([]string, 0, len(existing))
	removed := false
	for _, line := range existing {
		if strings.Contains(line, fullTag) {
			removed = true
			continue
		}
		filtered = append(filtered, line)
	}

	if !removed {
		return nil // 没找到，不必写回
	}

	if err := m.writeCrontab(filtered); err != nil {
		return fmt.Errorf("写入 crontab 失败: %w", err)
	}

	log.Infof("[SystemCron] - %s", tag)
	return nil
}

// CleanAll 清除所有标记条目
func (m *SystemCronManager) CleanAll() {
	existing, err := m.readCrontab()
	if err != nil {
		return
	}

	filtered := make([]string, 0, len(existing))
	for _, line := range existing {
		if strings.Contains(line, m.tagPrefix) {
			continue
		}
		filtered = append(filtered, line)
	}

	if len(filtered) != len(existing) {
		if err := m.writeCrontab(filtered); err != nil {
			log.Infof("[SystemCron] 清理失败: %v", err)
		}
	}
}

// readCrontab 读取当前用户的 crontab
func (m *SystemCronManager) readCrontab() ([]string, error) {
	cmd := exec.Command("crontab", "-l")
	out, err := cmd.Output()
	if err != nil {
		// crontab 为空时返回错误，这是正常情况
		return nil, nil
	}
	return strings.Split(strings.TrimSpace(string(out)), "\n"), nil
}

// writeCrontab 写入 crontab（完全覆盖）
func (m *SystemCronManager) writeCrontab(lines []string) error {
	// 过滤空行
	nonEmpty := make([]string, 0, len(lines))
	for _, l := range lines {
		if strings.TrimSpace(l) != "" {
			nonEmpty = append(nonEmpty, l)
		}
	}

	input := strings.Join(nonEmpty, "\n") + "\n"
	cmd := exec.Command("crontab", "-")
	cmd.Stdin = strings.NewReader(input)
	return cmd.Run()
}

// writeNotifyScript 写入通知脚本
func writeNotifyScript(path string) error {
	script := `#!/bin/bash
# 系统通知脚本
# 用法: notify.sh "消息内容" "标题"

BODY="${1:-提醒}"
TITLE="${2:-定时提醒}"

case "$(uname -s)" in
	Darwin)
		osascript -e "display notification \"$BODY\" with title \"$TITLE\"" 2>/dev/null
		;;
	Linux)
		if command -v notify-send &>/dev/null; then
			notify-send "$TITLE" "$BODY" 2>/dev/null
		elif [ -n "$DBUS_SESSION_BUS_ADDRESS" ]; then
			# 通过 dbus 直接发送
			gdbus call --session \
				--dest=org.freedesktop.Notifications \
				--object-path=/org/freedesktop/Notifications \
				--method=org.freedesktop.Notifications.Notify \
				"toolshub" 0 "" "$TITLE" "$BODY" \
				'[]' '{}' 5000 2>/dev/null || true
		else
			# 容器/无桌面环境：输出到日志
			echo "[$(date '+%Y-%m-%d %H:%M:%S')] $TITLE: $BODY"
		fi
		;;
esac
`
	return os.WriteFile(path, []byte(script), constants.FilePermDir)
}
