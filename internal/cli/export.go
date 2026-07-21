package cli

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
)

// REQ-F-039: export 命令 — 导出服务/扩展
var (
	exportExtension bool
	exportOutput    string
)

var exportCmd = &cobra.Command{
	Use:   "export <name>",
	Short: "导出服务或扩展",
	Long:  "导出指定服务或扩展的配置到文件。通过 HTTP API 与运行中的 supd 通信。",
	Args:  exactArgs(1),
	RunE:  runExport,
}

func init() {
	exportCmd.Flags().BoolVar(&exportExtension, "extension", false, "导出扩展而非服务")
	exportCmd.Flags().StringVarP(&exportOutput, "output", "o", "", "输出文件路径 (必填)")
}

// runExport 执行 export 命令
// REQ-F-039: supd export <name> -o <path> / supd export --extension <name> -o <path>
func runExport(cmd *cobra.Command, args []string) error {
	client := getAPIClient()
	if err := client.CheckSupdRunning(); err != nil {
		return err
	}

	if exportOutput == "" {
		return fmt.Errorf("必须指定 -o 输出路径")
	}

	name := args[0]
	var apiPath string
	if exportExtension {
		apiPath = fmt.Sprintf("/api/extensions/%s/export", name)
	} else {
		apiPath = fmt.Sprintf("/api/services/%s/export", name)
	}

	resp, err := client.Get(apiPath)
	if err != nil {
		return fmt.Errorf("导出 %s 失败: %w", name, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("导出 %s 失败: %s", name, string(body))
	}

	// 确保输出目录存在
	outDir := filepath.Dir(exportOutput)
	if err := os.MkdirAll(outDir, 0755); err != nil {
		return fmt.Errorf("创建输出目录失败: %w", err)
	}

	f, err := os.Create(exportOutput)
	if err != nil {
		return fmt.Errorf("创建输出文件失败: %w", err)
	}
	defer f.Close()

	if _, err := io.Copy(f, resp.Body); err != nil {
		return fmt.Errorf("写入输出文件失败: %w", err)
	}

	infof("已导出到 %s", exportOutput)
	return nil
}
