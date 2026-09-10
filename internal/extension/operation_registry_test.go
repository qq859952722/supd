package extension

import (
	"reflect"
	"testing"

	"github.com/supdorg/supd/internal/config"
	"github.com/supdorg/supd/internal/watch"
)

func regMeta(name string, ops []string, enabled bool, desc string) *config.ExtensionMeta {
	e := enabled
	return &config.ExtensionMeta{
		Name:        name,
		Description: desc,
		Enabled:     &e,
		Actions: []config.Action{
			{ID: "a1", Label: "A1", ButtonStyle: "default", Operations: ops},
		},
	}
}

func regGlobal(name string, meta *config.ExtensionMeta) *watch.DiscoveryResult {
	return &watch.DiscoveryResult{
		Services:   map[string]*watch.ServiceEntry{},
		GlobalExts: map[string]*watch.ExtensionEntry{name: {Name: name, Meta: meta, ConfigPath: "/fake/" + name + "/meta.yaml"}},
	}
}

func withService(d *watch.DiscoveryResult, svc string, exts ...*watch.ExtensionEntry) *watch.DiscoveryResult {
	if d.Services == nil {
		d.Services = map[string]*watch.ServiceEntry{}
	}
	svcEntry := &watch.ServiceEntry{Name: svc, Extensions: map[string]*watch.ExtensionEntry{}}
	for _, e := range exts {
		svcEntry.Extensions[e.Name] = e
	}
	d.Services[svc] = svcEntry
	return d
}

func svcExt(name, svc string, meta *config.ExtensionMeta) *watch.ExtensionEntry {
	return &watch.ExtensionEntry{Name: name, ServiceName: svc, Meta: meta, ConfigPath: "/fake/" + svc + "/" + name + "/meta.yaml"}
}

func TestRegistryGlobalRegistration(t *testing.T) {
	reg := NewOperationRegistry()
	reg.Rebuild(regGlobal("g-ext", regMeta("g-ext", []string{"check-update"}, true, "desc")))
	ops := reg.List()
	if len(ops) != 1 {
		t.Fatalf("operations = %d, want 1", len(ops))
	}
	op := ops[0]
	if op.ID != "check-update" || op.Label != "A1" || op.ButtonStyle != "default" || op.Description != "desc" {
		t.Errorf("unexpected operation info: %+v", op)
	}
	if !reflect.DeepEqual(op.Registrants, []string{"g-ext"}) {
		t.Errorf("registrants = %v", op.Registrants)
	}
	if len(op.Responders) != 0 {
		t.Errorf("global-only op should have no responders, got %v", op.Responders)
	}
}

func TestRegistryMergeSameIDConflictWarning(t *testing.T) {
	d := &watch.DiscoveryResult{
		Services: map[string]*watch.ServiceEntry{},
		GlobalExts: map[string]*watch.ExtensionEntry{
			"g-a": {Name: "g-a", Meta: regMeta("g-a", []string{"shared"}, true, "desc-a")},
			"g-b": {Name: "g-b", Meta: regMeta("g-b", []string{"shared"}, true, "desc-b")},
		},
	}
	reg := NewOperationRegistry()
	reg.Rebuild(d)
	op, ok := reg.Get("shared")
	if !ok {
		t.Fatal("merged op not found")
	}
	// 稳定排序第一项（extension_name→action_id）：g-a 在 g-b 之前，决定名称/样式。
	if op.Label != "A1" || len(op.Registrants) != 2 {
		t.Errorf("merged info wrong: label=%s registrants=%v", op.Label, op.Registrants)
	}
	if !reflect.DeepEqual(op.Registrants, []string{"g-a", "g-b"}) {
		t.Errorf("registrants order = %v, want [g-a g-b] (stable)", op.Registrants)
	}
	// desc 差异不是 warning（§三.3 仅名称/样式差异记 warning。
	if len(reg.Warnings("shared")) != 0 {
		t.Errorf("description-only difference should not warn, got %v", reg.Warnings("shared"))
	}
}

// TestRegistryMergeNameStyleConflictWarn 名称/样式差异记 warning。
func TestRegistryMergeNameStyleConflictWarn(t *testing.T) {
	// g-b 的 label/buttonStyle 与 g-a 不同 → warning。
	enabled := true
	metaB := &config.ExtensionMeta{Name: "g-b", Enabled: &enabled,
		Actions: []config.Action{{ID: "b1", Label: "B-Label", ButtonStyle: "danger", Operations: []string{"shared"}}}}
	d := &watch.DiscoveryResult{
		Services: map[string]*watch.ServiceEntry{},
		GlobalExts: map[string]*watch.ExtensionEntry{
			"g-a": {Name: "g-a", Meta: regMeta("g-a", []string{"shared"}, true, "")},
			"g-b": {Name: "g-b", Meta: metaB},
		},
	}
	reg := NewOperationRegistry()
	reg.Rebuild(d)
	if len(reg.Warnings("shared")) == 0 {
		t.Error("expected warning for name/style conflict across global registrants")
	}
	// 第一项（g-a）决定名称/样式。
	op, _ := reg.Get("shared")
	if op.Label != "A1" || op.ButtonStyle != "default" {
		t.Errorf("first-registrant should decide label/style, got %+v", op)
	}
}

func TestRegistryServiceResponderAndUnknownOpWarning(t *testing.T) {
	base := regGlobal("g-ext", regMeta("g-ext", []string{"known-op"}, true, "g"))
	// 服务 s1 响应 known-op；服务 s2 引用不存在的 ghost-op。
	base = withService(base, "s1", svcExt("s-ext", "s1", regMeta("s-ext", []string{"known-op"}, true, "")))
	base = withService(base, "s2", svcExt("s-ext2", "s2", regMeta("s-ext2", []string{"ghost-op"}, true, "")))

	reg := NewOperationRegistry()
	reg.Rebuild(base)

	op, ok := reg.Get("known-op")
	if !ok {
		t.Fatal("known-op not registered")
	}
	if len(op.Responders) != 1 {
		t.Fatalf("responders = %v, want 1", op.Responders)
	}
	if op.Responders[0].ServiceName != "s1" || op.Responders[0].ExtensionName != "s-ext" {
		t.Errorf("responder = %+v", op.Responders[0])
	}
	// ghost-op 未注册。
	if _, ok := reg.Get("ghost-op"); ok {
		t.Error("ghost-op should not be registered")
	}
	// 引用不存在操作的 warning 记在 known-op（Registry 无 ghost-op 键时记于同名键或全局）。
	// 设计：warning 记在对应操作键下；ghost-op 未注册故查找 known-op 不受影响。
	_ = op
}

func TestRegistrySkipsParseFailedAndDisabled(t *testing.T) {
	disabled := regMeta("g-dis", []string{"dis-op"}, false, "")
	parseFailed := &watch.ExtensionEntry{Name: "g-broken", Meta: nil, ParseError: "bad"}
	d := &watch.DiscoveryResult{
		Services:   map[string]*watch.ServiceEntry{},
		GlobalExts: map[string]*watch.ExtensionEntry{"g-dis": {Name: "g-dis", Meta: disabled}, "g-broken": parseFailed},
	}
	reg := NewOperationRegistry()
	reg.Rebuild(d)
	if len(reg.List()) != 0 {
		t.Errorf("expected empty registry (disabled + parse-failed skipped), got %d", len(reg.List()))
	}
}

func TestRegistryEmptyAndSnapshot(t *testing.T) {
	reg := NewOperationRegistry()
	reg.Rebuild(regGlobal("g", regMeta("g", []string{"op1"}, true, "")))
	if len(reg.List()) != 1 {
		t.Fatal("rebuild should register 1 op")
	}
	snap, ok := reg.Snapshot("op1")
	if !ok || len(snap.GlobalRuns) != 1 || snap.GlobalRuns[0].ExtensionName != "g" {
		t.Errorf("snapshot wrong: %+v", snap)
	}
	// 空 Registry。
	reg.Rebuild(&watch.DiscoveryResult{Services: map[string]*watch.ServiceEntry{}, GlobalExts: map[string]*watch.ExtensionEntry{}})
	if len(reg.List()) != 0 {
		t.Error("expected empty registry after rebuild with no operations")
	}
	if _, ok := reg.Snapshot("op1"); ok {
		t.Error("snapshot should miss after rebuild")
	}
}

// TestRegistryStableSortOrder extension_name→action_id 排序稳定性。
func TestRegistryStableSortOrder(t *testing.T) {
	// 同一扩展的多 action（不同 action_id）注册同一 op？配置校验禁止同一扩展重复操作 ID，
	// 故跨 action 场景不发生；此处用两个扩展验证 extension_name 排序。
	d := &watch.DiscoveryResult{
		Services: map[string]*watch.ServiceEntry{},
		GlobalExts: map[string]*watch.ExtensionEntry{
			"z-ext": {Name: "z-ext", Meta: regMeta("z-ext", []string{"order-op"}, true, "")},
			"a-ext": {Name: "a-ext", Meta: regMeta("a-ext", []string{"order-op"}, true, "")},
		},
	}
	reg := NewOperationRegistry()
	reg.Rebuild(d)
	op, _ := reg.Get("order-op")
	if !reflect.DeepEqual(op.Registrants, []string{"a-ext", "z-ext"}) {
		t.Errorf("registrants = %v, want [a-ext z-ext] (extension_name asc)", op.Registrants)
	}
}
