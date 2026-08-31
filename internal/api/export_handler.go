package api

import (
	"encoding/json"
	stderrors "errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/supdorg/supd/internal/archive"
	"github.com/supdorg/supd/internal/config"
	"github.com/supdorg/supd/internal/errors"
)

// REQ-I-006: 服务导入导出 API

// importLocks 按目标目录保护导入操作，防止并发导入同名服务/扩展导致目录损坏
// D-06-03: 键为目标目录绝对路径；条目不回收（数量受限于服务/扩展总数）
var importLocks sync.Map

// acquireImportLock 获取按 key 互斥的导入锁，返回释放函数
func acquireImportLock(key string) func() {
	v, _ := importLocks.LoadOrStore(key, &sync.Mutex{})
	mu := v.(*sync.Mutex)
	mu.Lock()
	return mu.Unlock
}

type importTransactionConfig struct {
	TargetDir string
	Reader    io.Reader
	Name      string
	KeepData  bool
	Validator func(string) error
}

type importTransactionMarker struct {
	TargetDir  string `json:"target_dir"`
	StagingDir string `json:"staging_dir"`
	BackupDir  string `json:"backup_dir,omitempty"`
	Phase      string `json:"phase"`
}

func executeImportTransaction(cfg importTransactionConfig) (backupDir string, err error) {
	parentDir := filepath.Dir(cfg.TargetDir)
	if err := os.MkdirAll(parentDir, 0755); err != nil {
		return "", fmt.Errorf("create target parent: %w", err)
	}
	stagingDir, err := os.MkdirTemp(parentDir, "."+cfg.Name+".staging-*")
	if err != nil {
		return "", fmt.Errorf("create staging: %w", err)
	}
	defer func() { _ = os.RemoveAll(stagingDir) }()

	if err := archive.UnpackDir(cfg.Reader, stagingDir); err != nil {
		return "", fmt.Errorf("unpack to staging: %w", err)
	}
	if cfg.KeepData {
		stagingData := filepath.Join(stagingDir, "data")
		if _, statErr := os.Stat(stagingData); os.IsNotExist(statErr) {
			targetData := filepath.Join(cfg.TargetDir, "data")
			if info, dataErr := os.Stat(targetData); dataErr == nil && info.IsDir() {
				if err := copyImportDir(targetData, stagingData); err != nil {
					return "", fmt.Errorf("preserve local data: %w", err)
				}
			}
		}
	}
	if cfg.Validator != nil {
		if err := cfg.Validator(stagingDir); err != nil {
			return "", fmt.Errorf("validate staging: %w", err)
		}
	}

	markerPath := filepath.Join(parentDir, "."+cfg.Name+".importing")
	marker := importTransactionMarker{TargetDir: cfg.TargetDir, StagingDir: stagingDir, Phase: "prepared"}
	if err := writeImportMarker(markerPath, marker); err != nil {
		return "", fmt.Errorf("write import marker: %w", err)
	}
	defer func() {
		if err == nil {
			_ = os.Remove(markerPath)
			return
		}
		if _, statErr := os.Stat(cfg.TargetDir); statErr == nil {
			_ = os.Remove(markerPath)
		}
	}()

	if _, statErr := os.Stat(cfg.TargetDir); statErr == nil {
		backupDir = fmt.Sprintf("%s.bak.%s", cfg.TargetDir, time.Now().Format("20060102T150405.000000000"))
		marker.BackupDir = backupDir
		if err := writeImportMarker(markerPath, marker); err != nil {
			return "", fmt.Errorf("update import marker: %w", err)
		}
		if err := os.Rename(cfg.TargetDir, backupDir); err != nil {
			return "", fmt.Errorf("backup target: %w", err)
		}
		marker.Phase = "backup_moved"
		if err := writeImportMarker(markerPath, marker); err != nil {
			if rollbackErr := os.Rename(backupDir, cfg.TargetDir); rollbackErr != nil {
				return "", fmt.Errorf("update import marker: %w; restore backup: %v", err, rollbackErr)
			}
			return "", fmt.Errorf("update import marker: %w", err)
		}
	} else if !os.IsNotExist(statErr) {
		return "", fmt.Errorf("stat target: %w", statErr)
	}

	if err := os.Rename(stagingDir, cfg.TargetDir); err != nil {
		if backupDir != "" {
			if rollbackErr := os.Rename(backupDir, cfg.TargetDir); rollbackErr != nil {
				return "", fmt.Errorf("activate staging: %w; restore backup: %v", err, rollbackErr)
			}
		}
		return "", fmt.Errorf("activate staging: %w", err)
	}
	return backupDir, nil
}

func writeImportMarker(path string, marker importTransactionMarker) error {
	data, err := json.Marshal(marker)
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".import-marker-*")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpPath, path)
}

// RecoverInterruptedImports 清理未切换的 staging，并在目标缺失时恢复完整备份。
func RecoverInterruptedImports(baseDir string) {
	for _, container := range []string{"services", "extensions"} {
		parentDir := filepath.Join(baseDir, container)
		markers, _ := filepath.Glob(filepath.Join(parentDir, ".*.importing"))
		for _, markerPath := range markers {
			data, readErr := os.ReadFile(markerPath)
			if readErr != nil {
				slog.Warn("read interrupted import marker failed", "path", markerPath, "error", readErr)
				continue
			}
			var marker importTransactionMarker
			if err := json.Unmarshal(data, &marker); err != nil {
				slog.Warn("parse interrupted import marker failed", "path", markerPath, "error", err)
				continue
			}
			if marker.StagingDir != "" {
				_ = os.RemoveAll(marker.StagingDir)
			}
			if _, err := os.Stat(marker.TargetDir); os.IsNotExist(err) && marker.BackupDir != "" {
				if _, backupErr := os.Stat(marker.BackupDir); backupErr == nil {
					if restoreErr := os.Rename(marker.BackupDir, marker.TargetDir); restoreErr != nil {
						slog.Error("restore interrupted import backup failed", "backup", marker.BackupDir, "target", marker.TargetDir, "error", restoreErr)
						continue
					}
				}
			}
			if err := os.Remove(markerPath); err != nil && !os.IsNotExist(err) {
				slog.Warn("remove interrupted import marker failed", "path", markerPath, "error", err)
			}
		}
		stagingDirs, _ := filepath.Glob(filepath.Join(parentDir, ".*.staging-*"))
		for _, stagingDir := range stagingDirs {
			if err := os.RemoveAll(stagingDir); err != nil {
				slog.Warn("cleanup interrupted import staging failed", "path", stagingDir, "error", err)
			}
		}
	}
}

func cleanupImportBackups(targetDir string, maxAge time.Duration) {
	matches, _ := filepath.Glob(targetDir + ".bak.*")
	cutoff := time.Now().Add(-maxAge)
	for _, backupDir := range matches {
		info, err := os.Stat(backupDir)
		if err != nil || !info.IsDir() || !info.ModTime().Before(cutoff) {
			continue
		}
		if err := os.RemoveAll(backupDir); err != nil {
			slog.Warn("cleanup expired import backup failed", "path", backupDir, "error", err)
		}
	}
}

func copyImportDir(src, dst string) error {
	return filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if info.IsDir() {
			return os.MkdirAll(target, info.Mode().Perm())
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("unsupported data entry: %s", rel)
		}
		in, err := os.Open(path)
		if err != nil {
			return err
		}
		defer in.Close()
		out, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, info.Mode().Perm())
		if err != nil {
			return err
		}
		_, copyErr := io.Copy(out, in)
		closeErr := out.Close()
		if copyErr != nil {
			return copyErr
		}
		return closeErr
	})
}

func validateServiceImport(stagingDir, expectedName string) error {
	svc, err := config.LoadService(filepath.Join(stagingDir, "service.yaml"))
	if err != nil {
		return err
	}
	if svc.Name != expectedName {
		return fmt.Errorf("service name mismatch: archive=%s request=%s", svc.Name, expectedName)
	}
	entries, err := os.ReadDir(filepath.Join(stagingDir, "extensions"))
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		meta, err := config.LoadExtension(filepath.Join(stagingDir, "extensions", entry.Name(), "meta.yaml"))
		if err != nil {
			return fmt.Errorf("extension %s: %w", entry.Name(), err)
		}
		if meta.Name != entry.Name() {
			return fmt.Errorf("extension directory/name mismatch: %s/%s", entry.Name(), meta.Name)
		}
		extDir := filepath.Join(stagingDir, "extensions", entry.Name())
		if err := validateExtensionForExport(extDir, filepath.Join(extDir, "meta.yaml"), extDir, ""); err != nil {
			return err
		}
	}
	return nil
}

func importErrorCode(err error) errors.ErrorCode {
	if stderrors.Is(err, archive.ErrPathTraversal) {
		return errors.ErrFileAccessDenied
	}
	message := err.Error()
	if strings.Contains(message, "validate staging:") {
		return errors.ErrServiceConfigInvalid
	}
	return errors.ErrInternal
}

func validateExtensionImport(stagingDir, expectedName, finalExtDir string) error {
	metaPath := filepath.Join(stagingDir, "meta.yaml")
	meta, err := config.LoadExtension(metaPath)
	if err != nil {
		return err
	}
	if meta.Name != expectedName {
		return fmt.Errorf("extension name mismatch: archive=%s request=%s", meta.Name, expectedName)
	}
	// entry 以扩展自身目录（stagingDir）为解析根
	return validateExtensionForExport(stagingDir, metaPath, stagingDir, finalExtDir)
}

// ImportPreviewResponse 导入预览响应
// REQ-F-038: 导入前对比版本号
type ImportPreviewResponse struct {
	Entries     []string            `json:"entries"`
	ServiceName string              `json:"service_name,omitempty"`
	ServiceInfo *ImportVersionInfo  `json:"service_info,omitempty"`
	Extensions  []ImportVersionInfo `json:"extensions,omitempty"`
	ExistsLocal bool                `json:"exists_local"`
}

// ImportVersionInfo 版本对比信息
type ImportVersionInfo struct {
	Name        string `json:"name"`
	ArchiveVer  string `json:"archive_version"`
	LocalVer    string `json:"local_version,omitempty"`
	ExistsLocal bool   `json:"exists_local"`
}

// handleExportService GET /api/services/{name}/export[?profile=<name>]
// REQ-I-006, REQ-F-038: 导出服务为tar.gz，支持按 profile 过滤
//
// profile 参数：
//   - 未指定或 "default"：使用 package.default.yaml（如存在），否则回退到 service.yaml 中的 package 配置，最终回退到内置默认（排除 data/）
//   - 其他名称：必须存在 package.<name>.yaml，否则返回 404
func (s *Server) handleExportService(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	if name == "" {
		respondError(w, errors.ErrInvalidRequest, "service name is required")
		return
	}

	if s.stateProvider != nil {
		if _, exists := s.stateProvider.GetServiceState(name); !exists {
			respondError(w, errors.ErrServiceNotFound, fmt.Sprintf("service %s not found", name))
			return
		}
	}

	// 生成服务目录路径
	var svcDir string
	if s.pathValidator != nil {
		svcDir = filepath.Join(s.pathValidator.baseDir, "services", name)
	} else {
		svcDir = filepath.Join("/etc/supd/services", name)
	}

	// 检查目录是否存在
	if _, err := os.Stat(svcDir); os.IsNotExist(err) {
		respondError(w, errors.ErrServiceNotFound, fmt.Sprintf("service directory %s not found", name))
		return
	}

	// 解析 profile 参数
	profileName := r.URL.Query().Get("profile")
	if profileName == "" {
		profileName = "default"
	}

	// 加载 service.yaml 中的 package 配置（用于 default 回退）
	var svcPackage *config.PackageConfig
	if sc, err := config.LoadService(filepath.Join(svcDir, "service.yaml")); err == nil {
		svcPackage = sc.Package
	}

	// 解析实际使用的 profile
	profile, source, err := config.ResolveExportProfile(svcDir, profileName, svcPackage)
	if err != nil {
		slog.Error("resolve export profile failed", "name", name, "profile", profileName, "error", err)
		respondError(w, errors.ErrFileNotFound, fmt.Sprintf("profile %q not found", profileName))
		return
	}

	slog.Info("export service", "name", name, "profile", profileName, "source", source)

	// 设置响应头
	w.Header().Set("Content-Type", "application/gzip")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%s.tar.gz", name))

	// 按 profile 打包服务目录
	if err := archive.PackDirWithProfile(svcDir, w, profile); err != nil {
		slog.Error("export service failed", "name", name, "profile", profileName, "error", err)
		return
	}
}

// ExportProfileInfo 导出 profile 信息
type ExportProfileInfo struct {
	Name        string `json:"name"`        // profile 名称
	HasFile     bool   `json:"has_file"`    // 是否存在 package.<name>.yaml 文件
	Description string `json:"description"` // 可选描述（从 profile 文件读取，目前留空）
}

// handleListExportProfiles GET /api/services/{name}/export-profiles
// 返回服务可用的导出 profile 列表。
// 始终包含 "default"（即使无文件，使用内置规则），另列出所有 package.<name>.yaml 文件。
func (s *Server) handleListExportProfiles(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	if name == "" {
		respondError(w, errors.ErrInvalidRequest, "service name is required")
		return
	}

	if s.stateProvider != nil {
		if _, exists := s.stateProvider.GetServiceState(name); !exists {
			respondError(w, errors.ErrServiceNotFound, fmt.Sprintf("service %s not found", name))
			return
		}
	}

	var svcDir string
	if s.pathValidator != nil {
		svcDir = filepath.Join(s.pathValidator.baseDir, "services", name)
	} else {
		svcDir = filepath.Join("/etc/supd/services", name)
	}

	if _, err := os.Stat(svcDir); os.IsNotExist(err) {
		respondError(w, errors.ErrServiceNotFound, fmt.Sprintf("service directory %s not found", name))
		return
	}

	profiles, err := config.ListPackageProfiles(svcDir)
	if err != nil {
		slog.Error("list export profiles failed", "name", name, "error", err)
		respondError(w, errors.ErrInternal, "failed to list export profiles")
		return
	}

	result := make([]ExportProfileInfo, 0, len(profiles))
	for _, p := range profiles {
		info := ExportProfileInfo{Name: p}
		// 检查文件是否存在
		filePath := filepath.Join(svcDir, config.PackageProfileFileName(p))
		if st, err := os.Stat(filePath); err == nil && !st.IsDir() {
			info.HasFile = true
		}
		result = append(result, info)
	}

	respondJSON(w, http.StatusOK, result)
}

// handleImportService POST /api/services/import
// REQ-I-006, REQ-F-038: 上传tar.gz预览导入内容，返回版本对比信息
func (s *Server) handleImportService(w http.ResponseWriter, r *http.Request) {
	// 限制上传大小
	maxSize := int64(MaxUploadSize) // O-05-002: 使用常量
	r.Body = http.MaxBytesReader(w, r.Body, maxSize)

	// 解析 multipart 表单
	if err := r.ParseMultipartForm(maxSize); err != nil {
		respondError(w, errors.ErrFileTooLarge, fmt.Sprintf("upload too large or invalid: %v", err))
		return
	}

	file, _, err := r.FormFile("file")
	if err != nil {
		respondError(w, errors.ErrInvalidRequest, "missing file in request")
		return
	}
	defer file.Close()

	// 读取文件内容到临时文件
	tmpFile, err := os.CreateTemp("", "supd-import-*.tar.gz")
	if err != nil {
		respondError(w, errors.ErrInternal, "failed to create temp file")
		return
	}
	defer os.Remove(tmpFile.Name())
	defer tmpFile.Close()

	if _, err := io.Copy(tmpFile, file); err != nil {
		respondError(w, errors.ErrInternal, "failed to save uploaded file")
		return
	}
	// M-01-001 修复：写入场景 Close 错误可能意味着数据未刷盘，文件可能损坏
	// 显式检查 Close 错误（不再走 defer tmpFile.Close()，因为我们要在错误时返回）
	// defer tmpFile.Close() 仍会执行，但 Close 错误已被本次调用消费（二次 Close 返回 nil 或 EBADF）
	if err := tmpFile.Close(); err != nil {
		respondError(w, errors.ErrInternal, fmt.Sprintf("failed to flush uploaded file: %v", err))
		return
	}

	// 列出tar.gz中的条目
	f, err := os.Open(tmpFile.Name())
	if err != nil {
		respondError(w, errors.ErrInternal, "failed to read uploaded file")
		return
	}
	entries, err := archive.ListEntries(f)
	// M-01-001 修复：读取场景 Close 错误无影响，但显式接收并日志记录
	if cerr := f.Close(); cerr != nil {
		slog.Warn("list entries file close failed", "error", cerr)
	}
	if err != nil {
		respondError(w, errors.ErrInvalidRequest, fmt.Sprintf("invalid archive: %v", err))
		return
	}

	// 读取 service.yaml 和 extensions/*/meta.yaml 的内容用于版本对比
	f2, err := os.Open(tmpFile.Name())
	if err != nil {
		respondError(w, errors.ErrInternal, "failed to read uploaded file")
		return
	}
	defer f2.Close()
	fileContents, err := archive.FileContentFromArchive(f2, []string{"service.yaml", "meta.yaml"})
	if err != nil {
		respondError(w, errors.ErrInvalidRequest, fmt.Sprintf("failed to read archive: %v", err))
		return
	}

	resp := ImportPreviewResponse{Entries: entries}

	// 解析 service.yaml 获取服务名和版本
	for path, data := range fileContents {
		if filepath.Base(path) == "service.yaml" {
			var svcCfg config.ServiceConfig
			// C-03-002 修复：解析失败时记录具体文件路径，便于用户定位导入错误
			if err := config.SafeUnmarshal(data, &svcCfg, config.DefaultSafeYAMLOptions); err != nil {
				slog.Warn("parse archive service.yaml failed", "archive_path", path, "error", err)
			} else {
				resp.ServiceName = svcCfg.Name
				resp.ServiceInfo = &ImportVersionInfo{
					Name:       svcCfg.Name,
					ArchiveVer: svcCfg.Version,
				}
				// 检查本地是否已存在
				if s.stateProvider != nil {
					if info, exists := s.stateProvider.GetServiceState(svcCfg.Name); exists {
						resp.ExistsLocal = true
						resp.ServiceInfo.ExistsLocal = true
						if info.Config != nil {
							resp.ServiceInfo.LocalVer = info.Config.Version
						}
					}
				}
			}
			break
		}
	}

	// 解析 extensions/*/meta.yaml 获取扩展版本
	for path, data := range fileContents {
		if filepath.Base(path) == "meta.yaml" && filepath.Dir(path) != "." {
			var meta config.ExtensionMeta
			// C-03-002 修复：解析失败时记录具体文件路径
			if err := config.SafeUnmarshal(data, &meta, config.DefaultSafeYAMLOptions); err != nil {
				slog.Warn("parse archive meta.yaml failed", "archive_path", path, "error", err)
			} else if meta.Name != "" {
				extInfo := ImportVersionInfo{
					Name:       meta.Name,
					ArchiveVer: meta.Version,
				}
				// 检查本地扩展是否存在
				if s.extProvider != nil {
					if localExt, ok := s.extProvider.GetExtension(meta.Name); ok {
						extInfo.ExistsLocal = true
						extInfo.LocalVer = localExt.Version
					}
				}
				resp.Extensions = append(resp.Extensions, extInfo)
			}
		}
	}

	respondJSON(w, http.StatusOK, resp)
}

// handleImportConfirm POST /api/services/import/confirm
// REQ-I-006, REQ-F-038: 确认导入服务
func (s *Server) handleImportConfirm(w http.ResponseWriter, r *http.Request) {
	// 限制上传大小
	maxSize := int64(MaxUploadSize) // O-05-002: 使用常量
	r.Body = http.MaxBytesReader(w, r.Body, maxSize)

	// 解析 multipart 表单
	if err := r.ParseMultipartForm(maxSize); err != nil {
		respondError(w, errors.ErrFileTooLarge, fmt.Sprintf("upload too large or invalid: %v", err))
		return
	}

	file, _, err := r.FormFile("file")
	if err != nil {
		respondError(w, errors.ErrInvalidRequest, "missing file in request")
		return
	}
	defer file.Close()

	// 从表单获取服务名
	name := r.FormValue("name")
	if name == "" {
		respondFieldErrors(w, errors.ErrInvalidRequest, "name is required",
			errors.FieldError{Field: "name", Message: "service name is required"})
		return
	}

	// H-03-001: 校验服务名，防止路径穿越（如 ../.. 逃逸 services/ 目录）
	// 要求与服务名 regex 一致：^[a-z][a-z0-9-]*$
	safeName := SanitizeFilename(name)
	if safeName != name || !isValidServiceName(name) {
		respondFieldErrors(w, errors.ErrInvalidRequest, "invalid service name",
			errors.FieldError{Field: "name", Message: "name must match ^[a-z][a-z0-9-]*$ and contain no path separators"})
		return
	}

	// 生成服务目录路径
	var svcDir string
	if s.pathValidator != nil {
		svcDir = filepath.Join(s.pathValidator.baseDir, "services", name)
	} else {
		svcDir = filepath.Join("/etc/supd/services", name)
	}

	// D-06-03: 按目标目录加锁，防止并发导入同名服务导致目录损坏/数据丢失
	// 锁覆盖备份-删除-重建-解包全过程；不同服务互不阻塞
	unlockImport := acquireImportLock(svcDir)
	defer unlockImport()

	if s.stateProvider != nil {
		if info, exists := s.stateProvider.GetServiceState(name); exists {
			status := string(info.State)
			if status != "down" && status != "failed" {
				respondError(w, errors.ErrServiceRunning, fmt.Sprintf("service %s is running (status: %s), stop it first", name, status))
				return
			}
		}
	}

	backupDir, err := executeImportTransaction(importTransactionConfig{
		TargetDir: svcDir,
		Reader:    file,
		Name:      name,
		KeepData:  true,
		Validator: func(stagingDir string) error { return validateServiceImport(stagingDir, name) },
	})
	if err != nil {
		slog.Error("service import transaction failed", "service", name, "error", err)
		code := importErrorCode(err)
		message := fmt.Sprintf("import failed: %v", err)
		if code == errors.ErrFileAccessDenied {
			message = fmt.Sprintf("archive contains path traversal: %v", err)
		}
		respondError(w, code, message)
		return
	}
	cleanupImportBackups(svcDir, 7*24*time.Hour)

	// R-001 修复：导入成功后显式触发热重载，避免依赖 fsnotify 异步检测的延迟和漏事件风险
	// 失败时不影响导入本身（导入已成功），仅记录 warn 日志
	resp := map[string]any{
		"name":       name,
		"message":    "service imported, hot-reload triggered",
		"backup_dir": backupDir,
	}
	newDiscovery, errCount, errDetails := s.triggerReload()
	if newDiscovery != nil {
		resp["services"] = len(newDiscovery.Services)
		resp["global_extensions"] = len(newDiscovery.GlobalExts)
		resp["scan_errors"] = errCount
		if errCount > 0 {
			resp["error_details"] = errDetails
			slog.Warn("service import triggered reload with scan errors", "name", name, "errors", errCount)
		}
	} else {
		slog.Warn("service import reload skipped: watch provider not configured", "name", name)
	}

	respondJSON(w, http.StatusCreated, resp)
}
