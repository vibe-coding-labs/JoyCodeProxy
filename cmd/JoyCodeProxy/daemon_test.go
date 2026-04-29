package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestDaemonPID_WriteRead(t *testing.T) {
	tmpDir := t.TempDir()
	pidFile := filepath.Join(tmpDir, "test.pid")

	original := daemonPID{
		PID:       12345,
		Port:      34891,
		StartedAt: time.Now().Format(time.RFC3339),
	}

	b, _ := json.MarshalIndent(original, "", "  ")
	if err := os.WriteFile(pidFile, b, 0644); err != nil {
		t.Fatalf("write failed: %v", err)
	}

	data, err := os.ReadFile(pidFile)
	if err != nil {
		t.Fatalf("read failed: %v", err)
	}
	var loaded daemonPID
	if err := json.Unmarshal(data, &loaded); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}

	if loaded.PID != original.PID {
		t.Errorf("PID = %d, want %d", loaded.PID, original.PID)
	}
	if loaded.Port != original.Port {
		t.Errorf("Port = %d, want %d", loaded.Port, original.Port)
	}
}

func TestCheckRunningDaemon_NoPIDFile(t *testing.T) {
	oldPIDFile := daemonPIDFile
	daemonPIDFile = filepath.Join(t.TempDir(), "nonexistent.pid")
	defer func() { daemonPIDFile = oldPIDFile }()

	pid, running := checkRunningDaemon()
	if running {
		t.Errorf("expected not running, got PID %d running=true", pid)
	}
}

func TestCheckRunningDaemon_StalePIDFile(t *testing.T) {
	tmpDir := t.TempDir()
	pidFile := filepath.Join(tmpDir, "stale.pid")

	data := daemonPID{PID: 999999998, Port: 34891, StartedAt: time.Now().Format(time.RFC3339)}
	b, _ := json.Marshal(data)
	os.WriteFile(pidFile, b, 0644)

	oldPIDFile := daemonPIDFile
	daemonPIDFile = pidFile
	defer func() { daemonPIDFile = oldPIDFile }()

	pid, running := checkRunningDaemon()
	if running {
		t.Errorf("expected stale PID to be detected, got PID %d running=true", pid)
	}
}

func TestSplitLines(t *testing.T) {
	tests := []struct {
		input  string
		expect int
	}{
		{"line1\nline2\nline3", 3},
		{"", 0},
		{"single", 1},
		{"a\nb\n", 2},
	}
	for _, tt := range tests {
		got := splitLines(tt.input)
		if len(got) != tt.expect {
			t.Errorf("splitLines(%q) = %d lines, want %d", tt.input, len(got), tt.expect)
		}
	}
}

func TestContainsStr(t *testing.T) {
	if !containsStr("hello world", "world") {
		t.Error("expected true for 'world' in 'hello world'")
	}
	if containsStr("hello", "world") {
		t.Error("expected false for 'world' in 'hello'")
	}
}

func TestMinDuration(t *testing.T) {
	if minDuration(1*time.Second, 2*time.Second) != 1*time.Second {
		t.Error("min(1s, 2s) should be 1s")
	}
	if minDuration(2*time.Second, 1*time.Second) != 1*time.Second {
		t.Error("min(2s, 1s) should be 1s")
	}
}
