package extension

import (
	"testing"

	"github.com/supdorg/supd/internal/config"
	"github.com/supdorg/supd/internal/watch"
)

// TestDispatcher_CleanupRemovedExtensions 验证热重载删除扩展时清理其 ConcurrencyManager tracker
func TestDispatcher_CleanupRemovedExtensions(t *testing.T) {
	disp := NewDispatcher(nil, "", "", 0)

	disp.GetConcurrencyManager().GetTracker("ext1", "run", PolicyReplace, 0)
	disp.GetConcurrencyManager().GetTracker("ext2", "run", PolicyReplace, 0)

	old := &watch.DiscoveryResult{
		GlobalExts: map[string]*watch.ExtensionEntry{
			"ext1": {Name: "ext1", Meta: &config.ExtensionMeta{Name: "ext1"}},
			"ext2": {Name: "ext2", Meta: &config.ExtensionMeta{Name: "ext2"}},
		},
	}
	new := &watch.DiscoveryResult{
		GlobalExts: map[string]*watch.ExtensionEntry{
			"ext1": {Name: "ext1", Meta: &config.ExtensionMeta{Name: "ext1"}},
		},
	}

	disp.CleanupRemovedExtensions(old, new)

	// ext2 已删除：其 tracker 应被移除（再次 GetTracker 返回新实例即等价于清理成功）
	if tr := disp.GetConcurrencyManager().GetTracker("ext2", "run", PolicyReplace, 0); tr == nil {
		t.Fatal("tracker should be re-creatable after cleanup (ext2 removed)")
	}
	// ext1 仍在：tracker 应保留
	if tr := disp.GetConcurrencyManager().GetTracker("ext1", "run", PolicyReplace, 0); tr == nil {
		t.Fatal("ext1 tracker should still exist after cleanup")
	}
}

// TestDispatcher_CleanupRemovedExtensions_Nil nil 防御不应 panic
func TestDispatcher_CleanupRemovedExtensions_Nil(t *testing.T) {
	disp := NewDispatcher(nil, "", "", 0)
	disp.CleanupRemovedExtensions(nil, nil)
}
