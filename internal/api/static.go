// REQ-F-002: 单二进制架构，前端通过 embed.FS 嵌入
// REQ-C-015: go build → 通过 //go:embed web/dist 嵌入前端

package api

import (
	"io/fs"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
)

// SetupStaticFiles 配置前端静态文件服务和 SPA fallback。
func SetupStaticFiles(r chi.Router, webFS http.FileSystem) {
	// 静态资源（JS/CSS/图片等）
	fileServer := http.FileServer(webFS)

	// 处理静态资源请求
	r.Get("/*", func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path

		// 尝试打开文件，如果存在则直接返回
		if f, err := webFS.Open(strings.TrimPrefix(path, "/")); err == nil {
			f.Close()
			fileServer.ServeHTTP(w, r)
			return
		}

		// SPA fallback：所有未匹配的路径返回 index.html
		r.URL.Path = "/"
		fileServer.ServeHTTP(w, r)
	})
}

// WebFSFromEmbed 将 embed.FS 转换为 http.FileSystem，用于静态文件服务。
// efs 应为 web.DistFS，其根目录下已有 dist 子目录。
func WebFSFromEmbed(efs fs.FS) http.FileSystem {
	sub, err := fs.Sub(efs, "dist")
	if err != nil {
		panic("dist not found in embed.FS: " + err.Error())
	}
	return http.FS(sub)
}
