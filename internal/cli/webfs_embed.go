//go:build !dev

package cli

import (
	"net/http"

	"github.com/supdorg/supd/internal/api"
	"github.com/supdorg/supd/web"
)

// getWebFS 返回嵌入的前端资源（非dev构建模式）
func getWebFS() http.FileSystem {
	return api.WebFSFromEmbed(web.DistFS)
}
