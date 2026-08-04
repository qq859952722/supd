package core

import (
	"path/filepath"
	"reflect"
	"testing"
)

func TestResolveCommandPath(t *testing.T) {
	root := t.TempDir()
	absolute := filepath.Join(t.TempDir(), "tool")
	tests := []struct {
		name string
		in   []string
		want []string
	}{
		{name: "empty", in: nil, want: nil},
		{name: "path command", in: []string{"sleep", "1"}, want: []string{"sleep", "1"}},
		{name: "dot relative", in: []string{"./bin/app", "arg"}, want: []string{filepath.Join(root, "bin", "app"), "arg"}},
		{name: "relative with separator", in: []string{"bin/app"}, want: []string{filepath.Join(root, "bin", "app")}},
		{name: "absolute", in: []string{absolute}, want: []string{absolute}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ResolveCommandPath(root, tt.in)
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("ResolveCommandPath() = %v, want %v", got, tt.want)
			}
		})
	}
}
