package api

import (
	"github.com/supdorg/supd/internal/watch"
)

// --- WatchProvider 适配器 ---

type CoreWatchProvider struct {
	Watcher   *watch.Watcher
	Discovery *watch.DiscoveryResult
	BaseDir   string
	LogDir    string
}

// SetDiscovery 热重载时更新 Discovery 引用
// N-04-001 修复：providers 持有 Discovery 指针值拷贝，reload 后需要显式更新
func (p *CoreWatchProvider) SetDiscovery(d *watch.DiscoveryResult) {
	if p == nil || d == nil {
		return
	}
	p.Discovery = d
}

func (p *CoreWatchProvider) ReloadConfig() error {
	disc := watch.NewDiscovery(p.BaseDir, p.LogDir)
	newDiscovery := disc.Scan()
	p.Discovery = newDiscovery
	return nil
}

func (p *CoreWatchProvider) GetDiscovery() *DiscoveryResultInfo {
	if p.Discovery == nil {
		return nil
	}

	result := &DiscoveryResultInfo{
		Services:   make(map[string]ServiceDiscoveryInfo),
		GlobalExts: make(map[string]ExtensionDiscoveryInfo),
	}

	for name, svc := range p.Discovery.Services {
		sdi := ServiceDiscoveryInfo{
			Name:       name,
			ConfigPath: svc.ConfigPath,
			Extensions: make(map[string]ExtensionDiscoveryInfo),
		}
		for extName, ext := range svc.Extensions {
			sdi.Extensions[extName] = ExtensionDiscoveryInfo{
				Name:        extName,
				ConfigPath:  ext.ConfigPath,
				ServiceName: name,
			}
		}
		result.Services[name] = sdi
	}

	for name, ext := range p.Discovery.GlobalExts {
		result.GlobalExts[name] = ExtensionDiscoveryInfo{
			Name:       name,
			ConfigPath: ext.ConfigPath,
		}
	}

	return result
}
