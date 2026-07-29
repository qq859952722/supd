package cli

import (
	"fmt"
	"io"
	"os"
	"runtime"

	"github.com/spf13/cobra"
)

// REQ-F-039: version 命令
// 版本信息通过 ldflags 注入
// REQ-C-015: 版本注入通过 ldflags
var (
	Version   = "dev"
	BuildTime = "unknown"
)

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "显示 supd 版本信息",
	RunE:  runVersion,
}

// runVersion 执行 version 命令
func runVersion(cmd *cobra.Command, args []string) error {
	var out io.Writer = os.Stdout
	if cmd != nil {
		out = cmd.OutOrStdout()
	}
	_, err := fmt.Fprintf(out, "supd %s\n  build time: %s\n  go version: %s\n  os/arch:    %s/%s\n", Version, BuildTime, runtime.Version(), runtime.GOOS, runtime.GOARCH)
	return err
}

// SetVersionInfo 设置版本信息，由 main 包调用
func SetVersionInfo(v, bt string) {
	Version = v
	BuildTime = bt
}
