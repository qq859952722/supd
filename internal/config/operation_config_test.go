package config

import (
	"testing"
)

func validOpMeta(actions ...Action) *ExtensionMeta {
	enabled := true
	return &ExtensionMeta{
		Name:           "op-ext",
		Version:        "1.0.0",
		Entry:          "run.sh",
		Enabled:        &enabled,
		TimeoutSeconds: 600,
		Concurrency:    "parallel",
		Actions:        actions,
	}
}

func opAction(id string, ops []string) Action {
	return Action{ID: id, Label: "Label " + id, ButtonStyle: "default", Operations: ops}
}

func TestValidateOperationsValid(t *testing.T) {
	meta := validOpMeta(
		opAction("check", []string{"check-update", "backup-data"}),
		opAction("update", []string{"update-software"}),
	)
	if err := ValidateExtension(meta); err != nil {
		t.Fatalf("valid operations rejected: %v", err)
	}
}

func TestValidateOperationsNilOK(t *testing.T) {
	// 未声明 operations（零值 nil）不影响既有校验。
	meta := validOpMeta(
		opAction("plain", nil),
	)
	if err := ValidateExtension(meta); err != nil {
		t.Fatalf("nil operations should be accepted: %v", err)
	}
}

func TestValidateOperationsRejectInvalidID(t *testing.T) {
	cases := []string{"Uppercase", "with_underscore", "", "9starts-digit", "has space", "with.dot"}
	for _, op := range cases {
		meta := validOpMeta(opAction("check", []string{op}))
		if err := ValidateExtension(meta); err == nil {
			t.Errorf("operation id %q should be rejected", op)
		}
	}
}

func TestValidateOperationsRejectDuplicateSameAction(t *testing.T) {
	meta := validOpMeta(opAction("check", []string{"dup-op", "dup-op"}))
	if err := ValidateExtension(meta); err == nil {
		t.Fatal("duplicate operation id within same action should be rejected")
	}
}

func TestValidateOperationsRejectDuplicateAcrossActions(t *testing.T) {
	// §三.3：同一扩展内（action 集合展平后）同一操作 ID只能出现一次。
	meta := validOpMeta(
		opAction("check", []string{"shared-op"}),
		opAction("update", []string{"shared-op"}),
	)
	if err := ValidateExtension(meta); err == nil {
		t.Fatal("duplicate operation id across actions in same extension should be rejected")
	}
}

func TestValidateOperationsAcceptValidUnderscoreFreeID(t *testing.T) {
	meta := validOpMeta(opAction("check", []string{"valid-op-id-1"}))
	if err := ValidateExtension(meta); err != nil {
		t.Fatalf("valid operation id rejected: %v", err)
	}
}
