package api

import (
	"strings"
	"testing"

	"github.com/supdorg/supd/internal/errors"
)

// TestValidateEntryPathValid 合法entry路径
func TestValidateEntryPathValid(t *testing.T) {
	tests := []struct {
		name  string
		entry string
	}{
		{"simple", "start.sh"},
		{"with path", "bin/start.sh"},
		{"with hyphen", "my-script.sh"},
		{"with underscore", "my_script.sh"},
		{"with dot", "script.v2.sh"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := ValidateEntryPath(tt.entry); err != nil {
				t.Errorf("ValidateEntryPath(%q) unexpected error: %v", tt.entry, err)
			}
		})
	}
}

// TestValidateEntryPathDotDot 路径包含..
func TestValidateEntryPathDotDot(t *testing.T) {
	err := ValidateEntryPath("../etc/passwd")
	if err == nil {
		t.Fatal("expected error for path with ..")
	}
	if serr, ok := err.(*errors.ServiceError); !ok || serr.Code != errors.ErrInvalidRequest {
		t.Errorf("expected ErrInvalidRequest, got %v", err)
	}
}

// TestValidateEntryPathShellMeta shell元字符
func TestValidateEntryPathShellMeta(t *testing.T) {
	tests := []struct {
		name  string
		entry string
		char  string
	}{
		{"semicolon", "cmd;rm -rf", ";"},
		{"pipe", "cmd|cat", "|"},
		{"ampersand", "cmd&bg", "&"},
		{"dollar", "$HOME/script", "$"},
		{"backtick", "`whoami`", "`"},
		{"newline", "cmd\nrm", "\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateEntryPath(tt.entry)
			if err == nil {
				t.Errorf("expected error for entry with %s", tt.char)
			}
		})
	}
}

// TestValidateEntryPathEmpty 空entry
func TestValidateEntryPathEmpty(t *testing.T) {
	err := ValidateEntryPath("")
	if err == nil {
		t.Fatal("expected error for empty entry")
	}
}

// TestSanitizeFilename 文件名清理
func TestSanitizeFilename(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"simple.txt", "simple.txt"},
		{"path/to/file.txt", "file.txt"},
		{"../secret.txt", "secret.txt"},
		{"..", ""},
		{".", ""},
		{"  .hidden", "hidden"},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := SanitizeFilename(tt.input)
			if got != tt.expected {
				t.Errorf("SanitizeFilename(%q) = %q, want %q", tt.input, got, tt.expected)
			}
		})
	}
}

// TestIsPathInBase 路径在基础目录下
func TestIsPathInBase(t *testing.T) {
	tests := []struct {
		name    string
		path    string
		baseDir string
		want    bool
	}{
		{"subdir", "/etc/supd/services/app.yaml", "/etc/supd", true},
		{"exact base", "/etc/supd", "/etc/supd", true},
		{"outside", "/etc/passwd", "/etc/supd", false},
		{"traversal", "/etc/supd/../passwd", "/etc/supd", false},
		{"empty path", "", "/etc/supd", false},
		{"empty base", "/etc/supd/services/app.yaml", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := IsPathInBase(tt.path, tt.baseDir)
			if got != tt.want {
				t.Errorf("IsPathInBase(%q, %q) = %v, want %v", tt.path, tt.baseDir, got, tt.want)
			}
		})
	}
}

// TestValidateRunAsUser run_as用户名校验
// REQ-E-005: run_as非root限制
func TestValidateRunAsUser(t *testing.T) {
	tests := []struct {
		name    string
		runAs   string
		wantErr bool
	}{
		{"empty allowed", "", false},
		{"normal user", "appuser", false},
		{"root denied", "root", true},
		{"with slash", "user/name", true},
		{"with dollar", "user$name", true},
		{"with semicolon", "user;name", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateRunAsUser(tt.runAs)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateRunAsUser(%q) error = %v, wantErr %v", tt.runAs, err, tt.wantErr)
			}
		})
	}
}

// TestValidateEntryPathCleanPath 清理后不一致的路径
func TestValidateEntryPathCleanPath(t *testing.T) {
	err := ValidateEntryPath("bin//start.sh")
	if err == nil {
		t.Fatal("expected error for path with redundant components")
	}
	if !strings.Contains(err.Error(), "redundant") {
		t.Errorf("expected redundant path error, got: %v", err)
	}
}
