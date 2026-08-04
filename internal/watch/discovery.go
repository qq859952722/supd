package watch

import (
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/supdorg/supd/internal/config"
)

// REQ-F-025: 文件发现规则
// 按需采集：服务、扩展、运行时通过目录扫描自动发现，不集中注册

// DiscoveryError 发现过程中的错误
type DiscoveryError struct {
	Path    string // 出错的文件/目录路径
	Message string // 错误信息
}

// REQ-F-025: 服务级扩展条目
type ExtensionEntry struct {
	Name        string                // 扩展名
	ConfigPath  string                // meta.yaml 路径
	EnvPath     string                // env.yaml 路径（可选）
	Meta        *config.ExtensionMeta // 解析后的配置（meta.yaml 解析失败时为 nil）
	ParseError  string                // meta.yaml 解析失败时的错误信息（Meta 为 nil 时填充）
	ServiceName string                // 服务级扩展所属的服务名，全局扩展为空
}

// REQ-F-025: 服务条目
type ServiceEntry struct {
	Name       string                     // 服务名
	ConfigPath string                     // service.yaml 路径
	EnvPath    string                     // env.yaml 路径（可选）
	Config     *config.ServiceConfig      // 解析后的配置
	Extensions map[string]*ExtensionEntry // 服务级扩展
}

// REQ-F-025: 发现结果
type DiscoveryResult struct {
	Services   map[string]*ServiceEntry   // key=服务名
	GlobalExts map[string]*ExtensionEntry // key=扩展名
	Runtimes   map[string]string          // key=运行时名, value=二进制路径
	Errors     []DiscoveryError           // 发现过程中的错误
}

// REQ-F-025: 文件发现器
type Discovery struct {
	baseDir       string   // /etc/supd/
	logDir        string   // /var/log/supd/
	extensionDirs []string // 全局扩展目录；相对路径基于 baseDir
}

// NewDiscovery 创建文件发现器。
// extensionDirs 省略时扫描默认 extensions/；配置项同时支持绝对路径和相对路径。
func NewDiscovery(baseDir, logDir string, extensionDirs ...string) *Discovery {
	if len(extensionDirs) == 0 {
		extensionDirs = []string{"extensions/"}
	}
	return &Discovery{
		baseDir:       baseDir,
		logDir:        logDir,
		extensionDirs: extensionDirs,
	}
}

// isBackupDir 判断目录名是否为删除扩展/服务时生成的备份目录。
// DeleteExtension 会将目录 rename 为 <name>.bak.<timestamp>，这类目录不应被扫描为有效扩展/服务，
// 否则会出现"删除扩展后列表里还有"的现象。
// 合法服务/扩展名只含 [a-z0-9-]（不含点），故含 ".bak" 的目录名一定是备份。
func isBackupDir(name string) bool {
	return strings.Contains(name, ".bak")
}

// Scan 执行全部5种发现规则
// REQ-F-025: 单个服务/扩展解析失败不影响其他，记录到 Errors
func (d *Discovery) Scan() *DiscoveryResult {
	result := &DiscoveryResult{
		Services:   make(map[string]*ServiceEntry),
		GlobalExts: make(map[string]*ExtensionEntry),
		Runtimes:   make(map[string]string),
	}

	d.discoverServices(result)
	d.discoverGlobalExtensions(result)
	d.discoverRuntimes(result)

	return result
}

// discoverServices 服务发现
// REQ-F-025: 扫描 baseDir/services/ 目录，每个子目录对应一个服务，子目录名即服务名，必须包含 service.yaml
func (d *Discovery) discoverServices(result *DiscoveryResult) {
	servicesDir := filepath.Join(d.baseDir, "services")

	entries, err := os.ReadDir(servicesDir)
	if err != nil {
		// 目录不存在不视为错误，返回空结果
		if os.IsNotExist(err) {
			return
		}
		result.Errors = append(result.Errors, DiscoveryError{
			Path:    servicesDir,
			Message: err.Error(),
		})
		return
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		svcName := entry.Name()
		// 跳过删除服务时生成的备份目录（<name>.bak.<timestamp>）
		if isBackupDir(svcName) {
			continue
		}
		svcDir := filepath.Join(servicesDir, svcName)
		configPath := filepath.Join(svcDir, "service.yaml")

		// REQ-F-025: 子目录中必须包含 service.yaml
		if _, err := os.Stat(configPath); err != nil {
			result.Errors = append(result.Errors, DiscoveryError{
				Path:    svcDir,
				Message: "missing service.yaml",
			})
			continue
		}

		// REQ-F-025: 调用 config.LoadService() 解析
		cfg, err := config.LoadService(configPath)
		if err != nil {
			result.Errors = append(result.Errors, DiscoveryError{
				Path:    configPath,
				Message: err.Error(),
			})
			continue
		}

		svcEntry := &ServiceEntry{
			Name:       svcName,
			ConfigPath: configPath,
			Config:     cfg,
			Extensions: make(map[string]*ExtensionEntry),
		}

		// REQ-F-025: 检查 env.yaml 是否存在，存在则记录路径
		envPath := filepath.Join(svcDir, "env.yaml")
		if _, err := os.Stat(envPath); err == nil {
			svcEntry.EnvPath = envPath
		}

		// REQ-F-025: 服务级扩展发现
		d.discoverServiceExtensions(svcEntry, result)

		result.Services[svcName] = svcEntry
	}
}

// discoverGlobalExtensions 全局扩展发现
// REQ-F-025: 扫描 baseDir/extensions/ 目录，每个子目录对应一个全局扩展，必须包含 meta.yaml
func (d *Discovery) discoverGlobalExtensions(result *DiscoveryResult) {
	for _, configuredDir := range d.extensionDirs {
		if configuredDir == "" {
			continue
		}
		extDir := config.ResolvePath(d.baseDir, configuredDir)
		d.discoverGlobalExtensionsDir(extDir, result)
	}
}

func (d *Discovery) discoverGlobalExtensionsDir(extDir string, result *DiscoveryResult) {
	entries, err := os.ReadDir(extDir)
	if err != nil {
		if os.IsNotExist(err) {
			return
		}
		result.Errors = append(result.Errors, DiscoveryError{
			Path:    extDir,
			Message: err.Error(),
		})
		return
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		extName := entry.Name()
		// 跳过删除扩展时生成的备份目录（<name>.bak.<timestamp>）
		if isBackupDir(extName) {
			continue
		}
		extSubDir := filepath.Join(extDir, extName)
		metaPath := filepath.Join(extSubDir, "meta.yaml")

		// REQ-F-025: 子目录中必须包含 meta.yaml
		if _, err := os.Stat(metaPath); err != nil {
			result.Errors = append(result.Errors, DiscoveryError{
				Path:    extSubDir,
				Message: "missing meta.yaml",
			})
			continue
		}

		meta, err := config.LoadExtension(metaPath)
		if err != nil {
			// R-06 修复：meta.yaml 解析失败时不丢弃扩展条目，保留 Entry（Meta=nil + ParseError）
			// 以便前端列表能看到该扩展并显示错误诊断，避免"扩展消失且无提示"的 UX 缺陷。
			// 同时仍记录到 DiscoveryResult.Errors 供扫描错误汇总。
			result.Errors = append(result.Errors, DiscoveryError{
				Path:    metaPath,
				Message: err.Error(),
			})
			extEntry := &ExtensionEntry{
				Name:       extName,
				ConfigPath: metaPath,
				Meta:       nil,
				ParseError: err.Error(),
			}
			// env.yaml 检查仍执行（解析失败也可能伴随 env.yaml，且不影响诊断）
			envPath := filepath.Join(extSubDir, "env.yaml")
			if _, err := os.Stat(envPath); err == nil {
				extEntry.EnvPath = envPath
			}
			result.GlobalExts[extName] = extEntry
			continue
		}

		extEntry := &ExtensionEntry{
			Name:       extName,
			ConfigPath: metaPath,
			Meta:       meta,
		}

		// REQ-F-025: 检查 env.yaml 是否存在
		envPath := filepath.Join(extSubDir, "env.yaml")
		if _, err := os.Stat(envPath); err == nil {
			extEntry.EnvPath = envPath
		}

		result.GlobalExts[extName] = extEntry
	}
}

// discoverRuntimes 运行时发现
// REQ-F-025: 扫描 baseDir/runtimes/ 目录，每个直接子文件视为运行时二进制
func (d *Discovery) discoverRuntimes(result *DiscoveryResult) {
	runtimesDir := filepath.Join(d.baseDir, "runtimes")

	entries, err := os.ReadDir(runtimesDir)
	if err != nil {
		if os.IsNotExist(err) {
			return
		}
		result.Errors = append(result.Errors, DiscoveryError{
			Path:    runtimesDir,
			Message: err.Error(),
		})
		return
	}

	for _, entry := range entries {
		// REQ-F-025: 每个文件（非目录）视为运行时，文件名即运行时名
		if entry.IsDir() {
			continue
		}

		rtName := entry.Name()
		rtPath := filepath.Join(runtimesDir, rtName)
		result.Runtimes[rtName] = rtPath
	}
}

// discoverServiceExtensions 服务级扩展发现
// REQ-F-025: 在服务目录的 extensions/ 子目录下扫描，每个子目录对应一个服务级扩展，必须包含 meta.yaml
func (d *Discovery) discoverServiceExtensions(svcEntry *ServiceEntry, result *DiscoveryResult) {
	svcExtDir := filepath.Join(filepath.Dir(svcEntry.ConfigPath), "extensions")

	entries, err := os.ReadDir(svcExtDir)
	if err != nil {
		// extensions/ 子目录不存在是正常的，不是错误
		if os.IsNotExist(err) {
			return
		}
		result.Errors = append(result.Errors, DiscoveryError{
			Path:    svcExtDir,
			Message: err.Error(),
		})
		return
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		extName := entry.Name()
		// 跳过删除扩展时生成的备份目录（<name>.bak.<timestamp>）
		if isBackupDir(extName) {
			continue
		}
		extSubDir := filepath.Join(svcExtDir, extName)
		metaPath := filepath.Join(extSubDir, "meta.yaml")

		// REQ-F-025: 必须包含 meta.yaml
		if _, err := os.Stat(metaPath); err != nil {
			result.Errors = append(result.Errors, DiscoveryError{
				Path:    extSubDir,
				Message: "missing meta.yaml",
			})
			continue
		}

		meta, err := config.LoadExtension(metaPath)
		if err != nil {
			// R-06 修复：同全局扩展，meta.yaml 解析失败时保留 Entry 供前端诊断
			result.Errors = append(result.Errors, DiscoveryError{
				Path:    metaPath,
				Message: err.Error(),
			})
			extEntry := &ExtensionEntry{
				Name:        extName,
				ConfigPath:  metaPath,
				Meta:        nil,
				ParseError:  err.Error(),
				ServiceName: svcEntry.Name,
			}
			envPath := filepath.Join(extSubDir, "env.yaml")
			if _, err := os.Stat(envPath); err == nil {
				extEntry.EnvPath = envPath
			}
			svcEntry.Extensions[extName] = extEntry
			continue
		}

		extEntry := &ExtensionEntry{
			Name:        extName,
			ConfigPath:  metaPath,
			Meta:        meta,
			ServiceName: svcEntry.Name,
		}

		// REQ-F-025: 检查 env.yaml 是否存在
		envPath := filepath.Join(extSubDir, "env.yaml")
		if _, err := os.Stat(envPath); err == nil {
			extEntry.EnvPath = envPath
		}

		svcEntry.Extensions[extName] = extEntry
	}
}

// DiscoverEnvFiles 环境变量文件发现
// REQ-F-025: 全局 baseDir/env/ 下所有 *.yaml 文件
func (d *Discovery) DiscoverEnvFiles() []string {
	envDir := filepath.Join(d.baseDir, "env")
	var envFiles []string

	entries, err := os.ReadDir(envDir)
	if err != nil {
		return envFiles
	}

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		if strings.HasSuffix(entry.Name(), ".yaml") {
			envFiles = append(envFiles, filepath.Join(envDir, entry.Name()))
		}
	}

	// 按文件名排序保证稳定顺序
	sort.Strings(envFiles)
	return envFiles
}
