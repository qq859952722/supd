package cli

import (
	"encoding/json"
	"fmt"
	"io"
)

type OutputFormat string

const (
	OutputTable OutputFormat = "table"
	OutputJSON  OutputFormat = "json"
)

func parseOutputFormat(value string) (OutputFormat, error) {
	if value == "" || value == string(OutputTable) {
		return OutputTable, nil
	}
	if value == string(OutputJSON) {
		return OutputJSON, nil
	}
	return "", fmt.Errorf("不支持的输出格式 %q（仅支持 table 或 json）", value)
}

func writeJSON(w io.Writer, value any) error {
	encoder := json.NewEncoder(w)
	encoder.SetIndent("", "  ")
	return encoder.Encode(value)
}
