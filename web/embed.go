//go:build !dev

// Package web 嵌入前端构建产物。
// REQ-F-002: 单二进制架构
// REQ-C-015: go build → 通过 //go:embed dist 嵌入前端
package web

import "embed"

// DistFS 包含前端构建产物，由 go build 时自动嵌入。
//
//go:embed dist
var DistFS embed.FS
