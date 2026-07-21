package main

// REQ-C-015: 版本注入通过 ldflags
// Build with: go build -ldflags "-X main.version=v1.0.0 -X main.buildTime=$(date -u +%Y%m%d)"

import (
	"fmt"
	"os"

	"github.com/supdorg/supd/internal/cli"
)

var (
	version   = "dev"
	buildTime = "unknown"
)

func main() {
	// REQ-F-039: CLI 入口
	// 将版本信息注入 CLI 包
	cli.SetVersionInfo(version, buildTime)

	if err := cli.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "错误: %v\n", err)
		os.Exit(1)
	}
}
