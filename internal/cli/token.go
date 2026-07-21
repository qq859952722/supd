package cli

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"
)

// REQ-F-039: token 子命令组 — Token 管理
var tokenCmd = &cobra.Command{
	Use:   "token",
	Short: "Token 管理",
	Long:  "管理 supd 认证 Token：生成、显示、验证。",
}

var tokenGenerateCmd = &cobra.Command{
	Use:   "generate",
	Short: "生成新 Token",
	RunE:  runTokenGenerate,
}

var tokenShowCmd = &cobra.Command{
	Use:   "show",
	Short: "显示当前 Token",
	Long:  "显示当前 Token。默认掩码显示，使用 --reveal 显示完整 Token。",
	RunE:  runTokenShow,
}

// H-05-001 修复：token show 默认掩码显示，--reveal 显示完整 Token
var tokenShowReveal bool

var tokenVerifyCmd = &cobra.Command{
	Use:   "verify <token>",
	Short: "验证 Token",
	Args:  exactArgs(1),
	RunE:  runTokenVerify,
}

func init() {
	tokenCmd.AddCommand(tokenGenerateCmd)
	tokenCmd.AddCommand(tokenShowCmd)
	tokenCmd.AddCommand(tokenVerifyCmd)
	// H-05-001 修复：添加 --reveal 标志
	tokenShowCmd.Flags().BoolVar(&tokenShowReveal, "reveal", false, "显示完整 Token（默认掩码显示）")
}

// REQ-F-039: supd token generate
func runTokenGenerate(cmd *cobra.Command, args []string) error {
	token, err := generateAuthToken()
	if err != nil {
		return fmt.Errorf("生成 Token 失败: %w", err)
	}
	infof("生成的 Token: %s", token)
	return nil
}

// REQ-F-039: supd token show
// H-05-001 修复：默认掩码显示，--reveal 显示完整 Token
func runTokenShow(cmd *cobra.Command, args []string) error {
	cfgPath := getConfigPath()

	data, err := readFile(cfgPath)
	if err != nil {
		return fmt.Errorf("读取配置文件失败: %w", err)
	}

	// 从配置中提取 auth_token
	token := extractAuthToken(data)
	if token == "" {
		return fmt.Errorf("配置文件中未找到 auth_token")
	}

	if tokenShowReveal {
		infof("当前 Token: %s", token)
	} else {
		infof("当前 Token: %s（使用 --reveal 查看完整 Token）", maskToken(token))
	}
	return nil
}

// maskToken 将 Token 掩码显示，仅保留前3位和后4位
// H-05-001 修复：避免在终端历史/屏幕录像中泄露完整 Token
func maskToken(token string) string {
	if len(token) <= 8 {
		return strings.Repeat("*", len(token))
	}
	return token[:3] + strings.Repeat("*", len(token)-7) + token[len(token)-4:]
}

// REQ-F-039: supd token verify <token>
// P-02-02: 改为 return error，由 cobra 统一处理（不再使用 os.Exit）
func runTokenVerify(cmd *cobra.Command, args []string) error {
	client := getAPIClient()
	token := args[0]

	body := map[string]string{"token": token}
	var result map[string]any
	if err := client.PostJSON("/api/auth/verify", body, &result); err != nil {
		return fmt.Errorf("token 验证失败: %w", err)
	}

	valid, _ := result["valid"].(bool)
	if valid {
		infof("Token 有效")
		return nil
	}
	return fmt.Errorf("token 无效")
}

// extractAuthToken 从配置内容中提取 auth_token
func extractAuthToken(data string) string {
	lines := strings.Split(data, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "auth_token:") {
			value := strings.TrimPrefix(line, "auth_token:")
			value = strings.TrimSpace(value)
			// 去掉 YAML 值的引号
			value = strings.Trim(value, `"`)
			return value
		}
	}
	return ""
}
