package cli

import (
	"encoding/json"
	"fmt"
	"io"
)

// OutputFormat CLI 输出格式。--output/-o 标志的解析结果。
type OutputFormat string

const (
	OutputTable OutputFormat = "table" // 人类可读的表格输出（默认）
	OutputJSON  OutputFormat = "json"  // 机器可读的 JSON 输出（便于脚本处理）
)

// parseOutputFormat 将 --output 标志值解析为 OutputFormat。
// 空字符串视为默认的 table。
func parseOutputFormat(value string) (OutputFormat, error) {
	if value == "" || value == string(OutputTable) {
		return OutputTable, nil
	}
	if value == string(OutputJSON) {
		return OutputJSON, nil
	}
	return "", fmt.Errorf("不支持的输出格式 %q（仅支持 table 或 json）", value)
}

// writeJSON 以 2 空格缩进编码 JSON 并写入 w，供 status 等命令的 JSON 输出复用。
func writeJSON(w io.Writer, value any) error {
	encoder := json.NewEncoder(w)
	encoder.SetIndent("", "  ")
	return encoder.Encode(value)
}
