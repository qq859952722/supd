package api

import (
	"os"
	"path/filepath"

	"github.com/supdorg/supd/internal/config"
	"github.com/supdorg/supd/internal/watch"
)

// --- RuntimeProvider 适配器 ---

type ConfigRuntimeProvider struct {
	Config  *config.Config
	BaseDir string
}

func (p *ConfigRuntimeProvider) ListRuntimes() []RuntimeInfo {
	discovery := watch.NewDiscovery(p.BaseDir, "")
	discResult := discovery.Scan()

	registry := config.BuildRegistry(p.Config.Runtimes, discResult.Runtimes)
	entries := config.List(registry)

	result := make([]RuntimeInfo, 0, len(entries))
	for _, e := range entries {
		result = append(result, RuntimeInfo{
			Alias:     e.Alias,
			Path:      e.AbsPath,
			Source:    string(e.Source),
			Available: e.Available,
		})
	}
	return result
}

func (p *ConfigRuntimeProvider) UploadRuntime(name string, data []byte) error {
	rtDir := filepath.Join(p.BaseDir, "runtimes")
	if err := os.MkdirAll(rtDir, 0755); err != nil {
		return err
	}
	rtPath := filepath.Join(rtDir, name)
	if err := os.WriteFile(rtPath, data, 0755); err != nil {
		return err
	}
	return os.Chmod(rtPath, 0755)
}

func (p *ConfigRuntimeProvider) DeleteRuntime(name string) error {
	rtPath := filepath.Join(p.BaseDir, "runtimes", name)
	return os.Remove(rtPath)
}
