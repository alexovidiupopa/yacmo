package logger

import "testing"

func TestParseLevel(t *testing.T) {
	tests := []struct {
		in   string
		want Level
	}{
		{"debug", DEBUG},
		{"DEBUG", DEBUG},
		{"info", INFO},
		{"warn", WARN},
		{"warning", WARN},
		{"error", ERROR},
		{"", INFO},         // unknown defaults to INFO
		{"nonsense", INFO}, // unknown defaults to INFO
	}
	for _, tt := range tests {
		if got := parseLevel(tt.in); got != tt.want {
			t.Errorf("parseLevel(%q) = %v, want %v", tt.in, got, tt.want)
		}
	}
}

func TestNewReturnsLogger(t *testing.T) {
	l := New("debug")
	if l == nil {
		t.Fatal("New returned nil")
	}
	if l.level != DEBUG {
		t.Errorf("level = %v, want DEBUG", l.level)
	}
	// Smoke test: these must not panic.
	l.Debug("d %d", 1)
	l.Info("i %s", "x")
	l.Warn("w")
	l.Error("e")
}
