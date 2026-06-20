package main

import (
	"testing"
)

func TestIsConvertibleFile(t *testing.T) {
	tests := []struct {
		path string
		want bool
	}{
		{"game.echoreplay", true},
		{"capture.nevrcap", true},
		{"old_format.tape", true},
		{"readme.txt", false},
		{"data.json", false},
		{"noext", false},
	}

	for _, tt := range tests {
		got := isConvertibleFile(tt.path)
		if got != tt.want {
			t.Errorf("isConvertibleFile(%q) = %v, want %v", tt.path, got, tt.want)
		}
	}
}
