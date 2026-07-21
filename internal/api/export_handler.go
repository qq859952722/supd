package api

import (
	stderrors "errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"sync"

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

// ImportPreviewResponse 导入预览响应
// REQ-F-038: 导入前对比版本号
type ImportPreviewResponse struct {
	Entries      []string             `json:"entries"`
	ServiceName  string               `json:"service_name,omitempty"`
	ServiceInfo  *ImportVersionInfo   `json:"service_info,omitempty"`
	Extensions   []ImportVersionInfo  `json:"extensions,omitempty"`
	ExistsLocal  bool                 `json:"exists_local"`
}

// ImportVersionInfo 版本对比信息
type ImportVersionInfo struct {
	Name         string `json:"name"`
	ArchiveVer   string `json:"archive_version"`
	LocalVer     string `json:"local_version,omitempty"`
	ExistsLocal  bool   `json:"exists_local"`
}

// handleExportService GET /api/services/{name}/export
// REQ-I-006, REQ-F-038: 导出服务为tar.gz
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

	// 设置响应头
	w.Header().Set("Content-Type", "application/gzip")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%s.tar.gz", name))

	// 打包服务目录
	if err := archive.PackDir(svcDir, w); err != nil {
		slog.Error("export service failed", "name", name, "error", err)
		return
	}
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

	// 如果服务已存在，先停止并删除旧目录（覆盖导入）
	// dataBackup 提升到外层作用域，供 R-003 回滚逻辑使用
	dataBackup := ""
	if s.stateProvider != nil {
		if info, exists := s.stateProvider.GetServiceState(name); exists {
			status := string(info.State)
			if status != "down" && status != "failed" {
				respondError(w, errors.ErrServiceRunning, fmt.Sprintf("service %s is running (status: %s), stop it first", name, status))
				return
			}
			// 删除旧目录（保留 data/）
			dataDir := filepath.Join(svcDir, "data")
			if st, err := os.Stat(dataDir); err == nil && st.IsDir() {
				dataBackup = svcDir + ".data-backup"
				// C-01-001 修复：删除旧备份失败时记录日志而非静默丢弃
				if err := os.RemoveAll(dataBackup); err != nil {
					slog.Warn("remove old data backup failed", "path", dataBackup, "error", err)
				}
				if err := os.Rename(dataDir, dataBackup); err != nil {
					slog.Error("backup data dir failed, aborting import", "service", name, "error", err)
					respondError(w, errors.ErrInternal, "failed to backup service data directory")
					return
				}
			}
			if err := os.RemoveAll(svcDir); err != nil {
				slog.Error("remove old service dir failed", "service", name, "error", err)
				respondError(w, errors.ErrInternal, "failed to remove old service directory")
				return
			}
			if dataBackup != "" {
				if err := os.MkdirAll(svcDir, 0755); err != nil {
					slog.Error("recreate service dir failed", "service", name, "error", err)
					respondError(w, errors.ErrInternal, "failed to recreate service directory")
					return
				}
				if err := os.Rename(dataBackup, dataDir); err != nil {
					slog.Error("restore data dir failed", "service", name, "error", err)
					respondError(w, errors.ErrInternal, "failed to restore service data directory")
					return
				}
				// data/ 已恢复到 svcDir/data/，清空 dataBackup 标记（不再需要回滚恢复）
				dataBackup = ""
			}
		}
	}

	// 创建服务目录
	if err := os.MkdirAll(svcDir, 0755); err != nil {
		respondError(w, errors.ErrInternal, fmt.Sprintf("failed to create service directory: %v", err))
		return
	}

	// 解包tar.gz到服务目录
	if err := archive.UnpackDir(file, svcDir); err != nil {
		// R-003 修复：解压失败时回滚，删除 svcDir 下除 data/ 外的残留文件，恢复 dataBackup（若存在）
		// 与扩展导入的原子性语义保持一致
		slog.Error("unpack archive failed, rolling back", "service", name, "dir", svcDir, "error", err)
		rollbackImportFailure(svcDir, dataBackup)
		// H-03-002 修复：zip slip（路径穿越）返回 403 而非 500
		if stderrors.Is(err, archive.ErrPathTraversal) {
			respondError(w, errors.ErrFileAccessDenied, fmt.Sprintf("archive contains path traversal: %v", err))
		} else {
			respondError(w, errors.ErrInternal, fmt.Sprintf("failed to unpack archive: %v", err))
		}
		return
	}

	// R-001 修复：导入成功后显式触发热重载，避免依赖 fsnotify 异步检测的延迟和漏事件风险
	// 失败时不影响导入本身（导入已成功），仅记录 warn 日志
	resp := map[string]any{
		"name":    name,
		"message": "service imported, hot-reload triggered",
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

// rollbackImportFailure R-003 修复：服务导入解压失败时回滚
// 删除 svcDir 下除 data/ 外的所有内容；若 dataBackup 存在则恢复为 data/
func rollbackImportFailure(svcDir, dataBackup string) {
	// 删除 svcDir 下除 data/ 外的所有内容（避免删除已恢复的 data/）
	entries, err := os.ReadDir(svcDir)
	if err != nil {
		slog.Warn("rollback: read service dir failed", "dir", svcDir, "error", err)
		return
	}
	for _, entry := range entries {
		if entry.Name() == "data" {
			continue
		}
		path := filepath.Join(svcDir, entry.Name())
		if rmErr := os.RemoveAll(path); rmErr != nil {
			slog.Warn("rollback: remove entry failed", "path", path, "error", rmErr)
		}
	}
	// 若存在 dataBackup，恢复为 data/
	if dataBackup != "" {
		dataDir := filepath.Join(svcDir, "data")
		if _, statErr := os.Stat(dataDir); statErr == nil {
			// data/ 已存在（恢复成功），删除 backup
			if rmErr := os.RemoveAll(dataBackup); rmErr != nil {
				slog.Warn("rollback: remove data backup failed", "backup", dataBackup, "error", rmErr)
			}
		} else {
			// data/ 不存在，从 backup 恢复
			if mvErr := os.Rename(dataBackup, dataDir); mvErr != nil {
				slog.Error("rollback: restore data dir from backup failed", "backup", dataBackup, "error", mvErr)
			}
		}
	}
}
