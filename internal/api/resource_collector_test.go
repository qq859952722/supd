package api

import (
	"math"
	"testing"
)

func TestParseNSpid(t *testing.T) {
	tests := []struct {
		name   string
		status string
		want   int
		ok     bool
	}{
		{name: "nested", status: "Name:\ttest\nNSpid:\t4321\t17\t3\n", want: 3, ok: true},
		{name: "single", status: "NSpid:\t42\n", want: 42, ok: true},
		{name: "missing", status: "Name:\ttest\nPid:\t42\n"},
		{name: "invalid", status: "NSpid:\t42\tinvalid\n"},
		{name: "empty", status: "NSpid:\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := parseNSpid([]byte(tt.status))
			if got != tt.want || ok != tt.ok {
				t.Fatalf("parseNSpid() = (%d, %v), want (%d, %v)", got, ok, tt.want, tt.ok)
			}
		})
	}
}

func TestSelectCommandProcessCandidate(t *testing.T) {
	candidate := func(hostPID, namespacePID int) commandProcessCandidate {
		return commandProcessCandidate{hostPID: hostPID, namespacePID: namespacePID}
	}
	tests := []struct {
		name       string
		candidates []commandProcessCandidate
		target     int
		wantHost   int
		ok         bool
	}{
		{name: "namespace exact", candidates: []commandProcessCandidate{candidate(100, 7), candidate(200, 8)}, target: 8, wantHost: 200, ok: true},
		{name: "host pid exact", candidates: []commandProcessCandidate{candidate(100, 0), candidate(200, 0)}, target: 100, wantHost: 100, ok: true},
		{name: "unique fallback", candidates: []commandProcessCandidate{candidate(100, 0)}, target: 7, wantHost: 100, ok: true},
		{name: "ambiguous", candidates: []commandProcessCandidate{candidate(100, 0), candidate(200, 0)}, target: 7},
		{name: "duplicate exact", candidates: []commandProcessCandidate{candidate(100, 7), candidate(200, 7)}, target: 7},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := selectCommandProcessCandidate(tt.candidates, tt.target)
			if ok != tt.ok || got.hostPID != tt.wantHost {
				t.Fatalf("selectCommandProcessCandidate() = (%d, %v), want (%d, %v)", got.hostPID, ok, tt.wantHost, tt.ok)
			}
		})
	}
}

func TestSummarizeProcesses(t *testing.T) {
	processes := []ProcessInfo{
		{CPUPercent: 12.5, MemoryMB: 128},
		{CPUPercent: 7.25, MemoryMB: 64},
	}
	resources := summarizeProcesses(processes, 1024*1024*1024)

	if resources.CPUPercent != 19.75 {
		t.Fatalf("CPUPercent = %v, want 19.75", resources.CPUPercent)
	}
	if resources.MemoryMB != 192 {
		t.Fatalf("MemoryMB = %v, want 192", resources.MemoryMB)
	}
	if resources.ProcessCount != 2 {
		t.Fatalf("ProcessCount = %d, want 2", resources.ProcessCount)
	}
	if math.Abs(resources.MemoryPercent-18.75) > 0.0001 {
		t.Fatalf("MemoryPercent = %v, want 18.75", resources.MemoryPercent)
	}
}

func TestSummarizeProcessesUnknownTotalMemory(t *testing.T) {
	resources := summarizeProcesses([]ProcessInfo{{MemoryMB: 10}}, 0)
	if resources.MemoryPercent != 0 {
		t.Fatalf("MemoryPercent = %v, want 0", resources.MemoryPercent)
	}
}
