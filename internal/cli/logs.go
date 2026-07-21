package cli

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/spf13/cobra"
)

// REQ-F-039: logs 命令 — 查看日志
var (
	logsFollow bool
	logsLines  int
	logsSince  string
)

var logsCmd = &cobra.Command{
	Use:   "logs <name>",
	Short: "查看服务日志",
	Long:  "查看指定服务的运行日志。通过 HTTP API 与运行中的 supd 通信。",
	Args:  exactArgs(1),
	RunE:  runLogs,
}

func init() {
	logsCmd.Flags().BoolVarP(&logsFollow, "follow", "f", false, "实时跟踪日志输出")
	logsCmd.Flags().IntVarP(&logsLines, "lines", "n", 100, "显示最近 N 行日志")
	logsCmd.Flags().StringVar(&logsSince, "since", "", "显示指定时间之后的日志 (如 1h, 30m)")
}

// runLogs 执行 logs 命令
// REQ-F-039: supd logs <name> [-f] [-n 500] [--since 1h]
func runLogs(cmd *cobra.Command, args []string) error {
	client := getAPIClient()
	if err := client.CheckSupdRunning(); err != nil {
		return err
	}

	name := args[0]

	if logsFollow {
		return followLogs(client, name, logsSince, logsLines)
	}

	path := fmt.Sprintf("/api/services/%s/logs?lines=%d", name, logsLines)
	if logsSince != "" {
		duration, err := parseDuration(logsSince)
		if err != nil {
			return fmt.Errorf("无效的 --since 值 %q: %w", logsSince, err)
		}
		sinceTime := time.Now().Add(-duration).Format(time.RFC3339)
		path += "&since=" + sinceTime
	}

	return fetchLogs(client, path)
}

func fetchLogs(client *APIClient, path string) error {
	resp, err := client.Get(path)
	if err != nil {
		return fmt.Errorf("获取日志失败: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return parseAPIError(resp.StatusCode, body)
	}

	_, err = io.Copy(os.Stdout, resp.Body)
	return err
}

// followLogs 实时跟踪日志输出。
// P-02-01: 使用 ticker 定期轮询获取增量日志（规格禁止 SSE/WebSocket，长轮询/短轮询是允许的）
// 通过跟踪已输出的行数，每次轮询只输出新增行，Ctrl+C 退出
func followLogs(client *APIClient, name string, since string, initialLines int) error {
	// 构建 since 查询参数
	var sinceParam string
	if since != "" {
		duration, err := parseDuration(since)
		if err != nil {
			return fmt.Errorf("无效的 --since 值 %q: %w", since, err)
		}
		sinceParam = "&since=" + time.Now().Add(-duration).Format(time.RFC3339)
	}

	// fetchAndPrintNew 获取日志并只输出新增行
	shownLines := 0
	fetchAndPrintNew := func(lines int) error {
		path := fmt.Sprintf("/api/services/%s/logs?lines=%d%s", name, lines, sinceParam)
		resp, err := client.Get(path)
		if err != nil {
			return fmt.Errorf("获取日志失败: %w", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(resp.Body)
			return parseAPIError(resp.StatusCode, body)
		}
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			return err
		}
		// 按行分割，只输出尚未显示的新行
		content := strings.TrimRight(string(body), "\n")
		if content == "" {
			return nil
		}
		allLines := strings.Split(content, "\n")
		if len(allLines) > shownLines {
			for _, line := range allLines[shownLines:] {
				fmt.Println(line)
			}
			shownLines = len(allLines)
		}
		return nil
	}

	// 首次获取
	if err := fetchAndPrintNew(initialLines); err != nil {
		return err
	}

	// 设置 Ctrl+C 信号处理
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	defer signal.Stop(sigCh)

	// 每 2 秒轮询一次新日志
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-sigCh:
			return nil
		case <-ticker.C:
			// 获取已显示行数 + 初始批次大小，确保捕获新增日志
			fetchLines := shownLines + initialLines
			if err := fetchAndPrintNew(fetchLines); err != nil {
				// 错误时输出到 stderr 但继续轮询
				fmt.Fprintf(os.Stderr, "获取日志失败: %v\n", err)
			}
		}
	}
}

// parseDuration 解析人类友好的时间字符串
func parseDuration(s string) (time.Duration, error) {
	s = strings.TrimSpace(s)
	if len(s) == 0 {
		return 0, fmt.Errorf("empty duration")
	}

	// 支持 h/m/s 后缀
	unit := s[len(s)-1:]
	value := s[:len(s)-1]

	switch unit {
	case "h":
		hours, err := time.ParseDuration(value + "h")
		return hours, err
	case "m":
		minutes, err := time.ParseDuration(value + "m")
		return minutes, err
	case "s":
		seconds, err := time.ParseDuration(value + "s")
		return seconds, err
	default:
		return time.ParseDuration(s)
	}
}
