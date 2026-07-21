package api

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/supdorg/supd/internal/config"
)

// --- FileProvider 适配器 ---

type OsFileProvider struct {
	BaseDir       string
	PathValidator *PathValidator
	HistoryDir    string // /var/lib/supd/history
	LogDir        string // 日志目录（添加到文件树根目录作为虚拟节点）
	// MaxVersions 文件历史版本上限
	// O-05-002 修复：从 config.Settings.FileHistoryVersions 读取，替代硬编码 50
	MaxVersions int
}

func (p *OsFileProvider) FileTree(basePath string) ([]FileTreeNode, error) {
	var rootPath string
	if basePath == "" {
		rootPath = p.BaseDir
	} else {
		validated, err := p.PathValidator.Validate(basePath)
		if err != nil {
			return nil, err
		}
		rootPath = validated
	}

	nodes, err := p.buildTree(rootPath)
	if err != nil {
		return nil, err
	}

	// H-03-003 配套修复：根目录 config.yaml 通过文件 API 拒绝访问（含 auth_token 等敏感信息），
	// 文件树中也应隐藏，避免用户点击后遇到 FILE_ACCESS_DENIED 错误。
	// 全局配置通过 GET/PUT /api/settings 端点编辑（settings_handler.go）。
	// 仅过滤根目录的 config.yaml，不影响 services/*/config.yaml 等。
	if basePath == "" {
		filtered := nodes[:0]
		for _, n := range nodes {
			if !n.IsDir && n.Name == "config.yaml" {
				continue
			}
			filtered = append(filtered, n)
		}
		nodes = filtered
	}

	// 请求根目录时，添加日志目录虚拟节点（如果配置了 LogDir 且不在 baseDir 下）
	if basePath == "" && p.LogDir != "" {
		logDirAbs, _ := filepath.Abs(p.LogDir)
		baseDirAbs, _ := filepath.Abs(p.BaseDir)
		if !strings.HasPrefix(logDirAbs, baseDirAbs) {
			if info, err := os.Stat(logDirAbs); err == nil && info.IsDir() {
				// 日志目录在 baseDir 外：移除 baseDir 下可能存在的同名旧 logs 目录
				// 避免用户看到空的旧日志目录而困惑
				filtered := nodes[:0]
				for _, n := range nodes {
					if n.Name == "logs" {
						continue // 跳过 baseDir 下的 logs 目录
					}
					filtered = append(filtered, n)
				}
				nodes = filtered

				// 添加虚拟 logs 节点指向实际日志目录
				logChildren, err := p.buildTree(logDirAbs)
				if err == nil {
					nodes = append(nodes, FileTreeNode{
						Name:     "logs",
						Path:     logDirAbs,
						IsDir:    true,
						Children: logChildren,
					})
				}
			}
		}
	}

	return nodes, nil
}

func (p *OsFileProvider) buildTree(dir string) ([]FileTreeNode, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}

	var nodes []FileTreeNode
	for _, entry := range entries {
		node := FileTreeNode{
			Name:  entry.Name(),
			Path:  filepath.Join(dir, entry.Name()),
			IsDir: entry.IsDir(),
		}
		if entry.IsDir() {
			children, err := p.buildTree(filepath.Join(dir, entry.Name()))
			if err != nil {
				children = nil
			}
			node.Children = children
		} else {
			// 获取文件大小
			if info, err := entry.Info(); err == nil {
				node.Size = info.Size()
			}
		}
		nodes = append(nodes, node)
	}
	return nodes, nil
}

func (p *OsFileProvider) ReadFile(path string) ([]byte, error) {
	return os.ReadFile(path)
}

func (p *OsFileProvider) WriteFile(path string, content []byte) error {
	// 先保存历史版本
	p.saveHistory(path)
	return os.WriteFile(path, content, 0644)
}

func (p *OsFileProvider) CreateFile(path string, content []byte) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	return os.WriteFile(path, content, 0644)
}

func (p *OsFileProvider) CreateDir(path string) error {
	return os.MkdirAll(path, 0755)
}

func (p *OsFileProvider) DeleteFile(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	if info.IsDir() {
		return os.RemoveAll(path)
	}
	return os.Remove(path)
}

func (p *OsFileProvider) MoveFile(oldPath, newPath string) error {
	if err := os.Rename(oldPath, newPath); err != nil {
		return err
	}
	// BUG-02 修复：同步移动 history 目录，使 move 后新文件仍能 rollback。
	// history 目录路径为 <HistoryDir>/<relPath>，move 后 relPath 改变，需要同步重命名。
	// 如果旧 history 目录不存在（文件从未 snapshot），静默跳过。
	if p.HistoryDir == "" || p.BaseDir == "" {
		return nil
	}
	oldRel, err := filepath.Rel(p.BaseDir, oldPath)
	if err != nil {
		return nil // 不影响主操作
	}
	newRel, err := filepath.Rel(p.BaseDir, newPath)
	if err != nil {
		return nil
	}
	oldHistDir := filepath.Join(p.HistoryDir, oldRel)
	newHistDir := filepath.Join(p.HistoryDir, newRel)
	// 仅在旧 history 存在且新 history 不存在时移动（避免覆盖）
	if _, statErr := os.Stat(oldHistDir); statErr == nil {
		// 确保新父目录存在
		if mkErr := os.MkdirAll(filepath.Dir(newHistDir), 0755); mkErr != nil {
			slog.Warn("create new history parent dir failed", "dir", filepath.Dir(newHistDir), "error", mkErr)
			return nil
		}
		// 如果新 history 已存在（罕见：覆盖现有文件），先删除旧的
		// C-01-002 修复：RemoveAll 错误记录日志，便于诊断文件历史迁移失败
		if _, statErr := os.Stat(newHistDir); statErr == nil {
			if rmErr := os.RemoveAll(newHistDir); rmErr != nil {
				slog.Warn("remove existing history dir failed", "dir", newHistDir, "error", rmErr)
			}
		}
		if renameErr := os.Rename(oldHistDir, newHistDir); renameErr != nil {
			slog.Warn("move history dir failed", "from", oldHistDir, "to", newHistDir, "error", renameErr)
		}
	}
	return nil
}

func (p *OsFileProvider) FileHistory(path string) ([]FileVersion, error) {
	if p.HistoryDir == "" {
		return nil, nil
	}

	relPath, err := filepath.Rel(p.BaseDir, path)
	if err != nil {
		return nil, err
	}
	histDir := filepath.Join(p.HistoryDir, relPath)

	entries, err := os.ReadDir(histDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	var versions []FileVersion
	for _, entry := range entries {
		info, err := entry.Info()
		if err != nil {
			continue
		}
		// 文件名格式: v001
		name := entry.Name()
		var ver int
		if n, err := fmt.Sscanf(name, "v%d", &ver); err != nil || n != 1 {
			slog.Warn("parse file version failed, skipping", "name", name, "err", err)
			continue
		}
		versions = append(versions, FileVersion{
			Version:   ver,
			Timestamp: info.ModTime(),
			Size:      info.Size(),
		})
	}

	sort.Slice(versions, func(i, j int) bool {
		return versions[i].Version < versions[j].Version
	})

	return versions, nil
}

func (p *OsFileProvider) RollbackFile(path string, version int) error {
	if p.HistoryDir == "" {
		return fmt.Errorf("history not configured")
	}

	relPath, err := filepath.Rel(p.BaseDir, path)
	if err != nil {
		return err
	}
	histDir := filepath.Join(p.HistoryDir, relPath)
	histFile := filepath.Join(histDir, fmt.Sprintf("v%03d", version))

	data, err := os.ReadFile(histFile)
	if err != nil {
		return fmt.Errorf("history version %d not found: %w", version, err)
	}

	return os.WriteFile(path, data, 0644)
}

func (p *OsFileProvider) ValidateFile(path string, content []byte) ([]ValidationError, error) {
	var errs []ValidationError

	// 根据文件扩展名选择校验方式
	ext := strings.ToLower(filepath.Ext(path))
	switch ext {
	case ".yaml", ".yml":
		// I-01-001 修复：移除内层 `var errs []ValidationError`，避免遮蔽外层变量
		// Safe parse check
		var raw any
		if err := config.SafeUnmarshal(content, &raw, config.DefaultSafeYAMLOptions); err != nil {
			errs = append(errs, ValidationError{Message: err.Error()})
		}
		// BUG-01 修复：原实现对 map[string]any 调用 StrictUnmarshal，
		// KnownFields(true) 对 map 无效（map 接受任意字段），无法检测未知字段。
		// 改为根据文件 basename 推断配置类型，用对应 struct 严格校验。
		if len(errs) == 0 {
			if strictErr := strictValidateByFileName(path, content); strictErr != nil {
				errs = append(errs, ValidationError{Message: strictErr.Error()})
			}
		}
	default:
		// 非 YAML 文件不做校验
	}

	return errs, nil
}

// strictValidateByFileName 根据文件 basename 推断配置类型，对已知配置文件用对应
// struct 严格校验未知字段。未知类型（非 service.yaml/meta.yaml/env.yaml/config.yaml）
// 只做语法校验，不做字段校验。
func strictValidateByFileName(path string, content []byte) error {
	base := strings.ToLower(filepath.Base(path))
	switch base {
	case "service.yaml":
		var sc config.ServiceConfig
		return config.StrictUnmarshal(content, &sc, config.DefaultSafeYAMLOptions)
	case "meta.yaml":
		var em config.ExtensionMeta
		return config.StrictUnmarshal(content, &em, config.DefaultSafeYAMLOptions)
	case "env.yaml":
		var ef config.EnvFile
		return config.StrictUnmarshal(content, &ef, config.DefaultSafeYAMLOptions)
	case "config.yaml":
		var cfg config.Config
		return config.StrictUnmarshal(content, &cfg, config.DefaultSafeYAMLOptions)
	}
	return nil
}

func (p *OsFileProvider) SnapshotFile(path string) error {
	p.saveHistory(path)
	return nil
}

func (p *OsFileProvider) saveHistory(path string) {
	if p.HistoryDir == "" {
		return
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return
	}

	relPath, err := filepath.Rel(p.BaseDir, path)
	if err != nil {
		return
	}
	histDir := filepath.Join(p.HistoryDir, relPath)
	if err := os.MkdirAll(histDir, 0755); err != nil {
		slog.Warn("create history dir failed", "dir", histDir, "err", err)
		return
	}

	// 找到下一个版本号（基于已分配的最大版本号，而非文件数，避免淘汰后版本号冲突）
	entries, _ := os.ReadDir(histDir)
	maxVer := 0
	for _, e := range entries {
		var ver int
		if _, err := fmt.Sscanf(e.Name(), "v%d", &ver); err == nil && ver > maxVer {
			maxVer = ver
		}
	}
	nextVer := maxVer + 1
	histFile := filepath.Join(histDir, fmt.Sprintf("v%03d", nextVer))
	if err := os.WriteFile(histFile, data, 0644); err != nil {
		slog.Warn("write history file failed", "file", histFile, "error", err)
	}

	// REQ-2.3.1: 文件历史版本上限（默认50，可通过 config.Settings.file_history_versions 配置）
	// O-05-002 修复：读取配置替代硬编码 50
	maxVersions := p.MaxVersions
	if maxVersions <= 0 {
		maxVersions = 50 // 默认值（兜底，正常应从配置传入）
	}
	// 重新读取目录（含刚写入的新版本），超限时删除最旧版本
	if entries, err := os.ReadDir(histDir); err == nil && len(entries) > maxVersions {
		sort.Slice(entries, func(i, j int) bool {
			return entries[i].Name() < entries[j].Name()
		})
		// 删除最旧的 (len - maxVersions) 个版本
		// C-01-006/I-03-002 修复：Remove 错误记录日志，便于诊断历史版本清理失败
		for i := 0; i < len(entries)-maxVersions; i++ {
			if rmErr := os.Remove(filepath.Join(histDir, entries[i].Name())); rmErr != nil {
				slog.Warn("remove old history version failed", "file", entries[i].Name(), "error", rmErr)
			}
		}
	}
}
