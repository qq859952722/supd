package watch

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
)

// DefaultDebounceInterval fsnotify 事件防抖间隔
// REQ-2.4.2: 数值锁定 500ms
const DefaultDebounceInterval = 500 * time.Millisecond

// REQ-F-026: fsnotify 监听器，监听 /etc/supd/ 下所有子目录
// 防抖500ms，按文件路径去重，原子写关注 rename 事件
type Watcher struct {
	fswatcher *fsnotify.Watcher   // fsnotify 实例
	debouncer *Debouncer          // 500ms 防抖器
	baseDir   string              // 监控的根目录
	done      chan struct{}       // 停止信号
	stopOnce  sync.Once           // A-08-001: 保护 Stop 仅执行一次，避免重复 close panic

	// A-08-001 修复：运行时 fd 耗尽检测
	// 连续 addWatch 失败计数，超过阈值时发出警告（疑似 fd 耗尽）
	// 成功添加监控时计数清零
	consecutiveAddFailures int
	addFailMu              sync.Mutex
}

// A-08-001 修复：连续 addWatch 失败阈值
// 超过此阈值时判定为疑似 fd 耗尽（EMFILE/ENFILE），发出 slog.Warn
// 阈值选取依据：正常情况下 addWatch 不应连续失败，5 次连续失败几乎可以确认是系统级资源问题
const consecutiveAddFailureThreshold = 5

// NewWatcher 创建 fsnotify 监听器
// REQ-F-026: baseDir 为 supd 基础目录（如 /etc/supd/），
// 递归添加 baseDir 下所有子目录到监控，防抖间隔 500ms
func NewWatcher(baseDir string) (*Watcher, error) {
	fsw, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, fmt.Errorf("REQ-F-026: create fsnotify watcher: %w", err)
	}

	// REQ-F-026: 防抖间隔 500ms（数值锁定）
	debouncer := NewDebouncer(DefaultDebounceInterval)

	w := &Watcher{
		fswatcher: fsw,
		debouncer: debouncer,
		baseDir:   baseDir,
		done:      make(chan struct{}),
	}

	// REQ-F-026: 递归添加 baseDir 下所有子目录到监控
	w.walkAndWatch()

	return w, nil
}

// Start 启动监控
// REQ-F-026: 启动 debouncer 和事件转发 goroutine
func (w *Watcher) Start() {
	w.debouncer.Start()
	go w.forwardEvents()
}

// Stop 停止监控
// REQ-F-026: 关闭 fsnotify.Watcher，停止 debouncer
// A-08-001: 使用 sync.Once 保护，避免重复调用导致 close panic
func (w *Watcher) Stop() {
	w.stopOnce.Do(func() {
		close(w.done)
		w.fswatcher.Close()
		w.debouncer.Stop()
	})
}

// Events 获取去重后的事件批次通道
// REQ-F-026: 返回 debouncer 的去重事件批次
func (w *Watcher) Events() <-chan []ChangeEvent {
	return w.debouncer.Events()
}

// forwardEvents 事件转发 goroutine
// REQ-F-026: fsnotify events → ChangeEvent → debouncer.Push()
// fsnotify 事件映射：Create→"create", Write→"write", Rename→"rename", Remove→"remove"
// 新建子目录时自动添加监控
func (w *Watcher) forwardEvents() {
	for {
		select {
		case <-w.done:
			return

		case event, ok := <-w.fswatcher.Events:
			if !ok {
				return
			}
			ce := ChangeEvent{
				Path:      event.Name,
				Timestamp: time.Now(),
			}

			// REQ-F-026: fsnotify 事件映射
			switch {
			case event.Has(fsnotify.Create):
				ce.Operation = "create"
				// REQ-F-026: 新建子目录时自动添加监控
				w.handleNewDir(event.Name)
			case event.Has(fsnotify.Write):
				ce.Operation = "write"
			case event.Has(fsnotify.Rename):
				// REQ-F-026: 原子写关注 rename 事件
				ce.Operation = "rename"
			case event.Has(fsnotify.Remove):
				// REQ-F-026: 删除事件不报错，服务/扩展目录被删→卸载该服务/扩展
				ce.Operation = "remove"
			default:
				continue
			}

			w.debouncer.Push(ce)

		case <-w.fswatcher.Errors:
			// REQ-F-026: fsnotify 错误不 panic，忽略
		}
	}
}

// handleNewDir 检查新建路径是否为目录，如果是则递归添加监控
// REQ-F-026: 新建子目录时自动添加监控（仅白名单目录）
func (w *Watcher) handleNewDir(path string) {
	info, err := os.Stat(path)
	if err != nil {
		return
	}
	if !info.IsDir() {
		return
	}
	// 白名单过滤：只监控配置目录
	if !shouldWatchDir(w.baseDir, path) {
		return
	}
	// 递归添加该目录及其子目录
	if err := filepath.WalkDir(path, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			slog.Warn("walk new dir entry error", "path", p, "err", err)
			return nil
		}
		if d.IsDir() && shouldWatchDir(w.baseDir, p) {
			w.addWatch(p)
		}
		return nil
	}); err != nil {
		slog.Warn("walk new dir failed", "path", path, "err", err)
	}
}

// addWatch 添加监控路径，记录错误不 panic
// REQ-F-026: 监听 /etc/supd/ 下所有子目录
//
// A-08-001 修复：运行时 fd 耗尽检测
// 通过追踪连续 addWatch 失败次数，识别运行时 fd 耗尽（EMFILE/ENFILE）
// 超过阈值时发出 slog.Warn，提示用户检查系统 inotify/max_user_watches 或 fd ulimit
// 注意：此处不切换到 FallbackWatcher 模式（已监控的目录仍生效），仅发出警告
func (w *Watcher) addWatch(path string) {
	if err := w.fswatcher.Add(path); err != nil {
		// REQ-F-026: 添加监控失败不 panic，记录错误即可
		fmt.Fprintf(os.Stderr, "REQ-F-026: add watch %s: %v\n", path, err)

		// A-08-001: 连续失败计数 + 阈值检测
		w.addFailMu.Lock()
		w.consecutiveAddFailures++
		count := w.consecutiveAddFailures
		w.addFailMu.Unlock()

		if count == consecutiveAddFailureThreshold {
			// 仅在首次达到阈值时发出警告，避免日志刷屏
			// 后续仍会继续尝试 addWatch（已有 watcher 实例仍在工作）
			slog.Warn("consecutive addWatch failures reached threshold, possible fd exhaustion",
				"threshold", consecutiveAddFailureThreshold,
				"hint", "check /proc/sys/fs/inotify/max_user_watches and ulimit -n",
				"last_error", err)
		}
		return
	}

	// 成功添加监控，清零连续失败计数
	w.addFailMu.Lock()
	w.consecutiveAddFailures = 0
	w.addFailMu.Unlock()
}

// walkAndWatch 递归添加 baseDir 下白名单子目录到监控
// REQ-F-026: 监听 /etc/supd/ 下配置文件目录
func (w *Watcher) walkAndWatch() {
	if err := filepath.WalkDir(w.baseDir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			slog.Warn("walk base dir entry error", "path", path, "err", err)
			return nil
		}
		if !d.IsDir() {
			return nil
		}
		// 跳过不需要遍历的目录（性能优化，避免遍历 data/bin/logs 等运行时数据目录）
		if shouldSkipDir(w.baseDir, path) {
			return filepath.SkipDir
		}
		// 白名单过滤：只监控配置目录
		if shouldWatchDir(w.baseDir, path) {
			w.addWatch(path)
		}
		return nil
	}); err != nil {
		slog.Warn("walk base dir failed", "baseDir", w.baseDir, "error", err)
	}
}

// shouldSkipDir 判断目录是否应该跳过遍历
// 用于性能优化，避免遍历运行时数据目录（如 data/bin/logs 等）
// 注意：返回 true 表示跳过该目录及其所有子目录
func shouldSkipDir(baseDir, path string) bool {
	if path == baseDir {
		return false
	}
	base := filepath.Base(path)
	// 跳过隐藏目录（.supd, .git 等）
	if strings.HasPrefix(base, ".") {
		return true
	}
	// 跳过已知运行时数据目录
	switch base {
	case "data", "bin", "logs", "history", "cache", "tmp", "temp", "run":
		return true
	}
	return false
}

// shouldWatchDir 判断目录是否应该被监控
// 白名单策略：只监控配置文件（service.yaml/meta.yaml/config.yaml/env.yaml）所在目录、
// 其父目录（容器目录）、以及 env/ 目录
// 这样可以避免监控服务运行时数据目录（如 qbittorrent 的 data/qbittorrent/qBittorrent/config/）
func shouldWatchDir(baseDir, path string) bool {
	// 根目录监控（config.yaml）
	if path == baseDir {
		return true
	}
	// 跳过隐藏目录和运行时数据目录
	if shouldSkipDir(baseDir, path) {
		return false
	}
	// 计算相对路径
	rel, err := filepath.Rel(baseDir, path)
	if err != nil {
		return false
	}
	parts := strings.Split(rel, string(filepath.Separator))
	// 容器目录监控（捕获新服务/扩展创建）
	if len(parts) == 1 {
		switch parts[0] {
		case "env", "services", "extensions":
			return true
		}
	}
	// services/<name>/ 监控（service.yaml, env.yaml）
	if len(parts) == 2 && parts[0] == "services" {
		return true
	}
	// services/<name>/extensions/ 容器目录监控
	if len(parts) == 3 && parts[0] == "services" && parts[2] == "extensions" {
		return true
	}
	// services/<name>/extensions/<ext>/ 监控（meta.yaml）
	if len(parts) == 4 && parts[0] == "services" && parts[2] == "extensions" {
		return true
	}
	// extensions/<name>/ 监控（meta.yaml）
	if len(parts) == 2 && parts[0] == "extensions" {
		return true
	}
	return false
}
