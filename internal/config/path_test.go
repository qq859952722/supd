package config

import (
	"path/filepath"
	"testing"
)

func TestResolvePath(t *testing.T) {
	root := t.TempDir()
	absolute := filepath.Join(t.TempDir(), "bin", "tool")
	tests := []struct {
		name string
		path string
		want string
	}{
		{name: "empty", path: "", want: ""},
		{name: "relative", path: "bin/tool", want: filepath.Join(root, "bin", "tool")},
		{name: "dot relative", path: "./bin/tool", want: filepath.Join(root, "bin", "tool")},
		{name: "parent relative", path: "../tool", want: filepath.Clean(filepath.Join(root, "../tool"))},
		{name: "dot", path: ".", want: root},
		{name: "absolute", path: absolute, want: absolute},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ResolvePath(root, tt.path); got != tt.want {
				t.Fatalf("ResolvePath(%q, %q) = %q, want %q", root, tt.path, got, tt.want)
			}
		})
	}
}
