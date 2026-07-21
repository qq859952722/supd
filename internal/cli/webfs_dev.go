//go:build dev

package cli

import "net/http"

// getWebFS 返回 nil（dev模式不嵌入前端，使用 vite dev server）
func getWebFS() http.FileSystem {
	return nil
}
