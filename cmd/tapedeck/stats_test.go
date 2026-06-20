package main

import (
	"testing"
)

func TestTruncate_ASCII(t *testing.T) {
	got := truncate("hello world", 5)
	if got != "hell~" {
		t.Errorf("truncate(\"hello world\", 5) = %q, want %q", got, "hell~")
	}
}

func TestTruncate_ShortString(t *testing.T) {
	got := truncate("hi", 10)
	if got != "hi" {
		t.Errorf("truncate(\"hi\", 10) = %q, want %q", got, "hi")
	}
}

func TestTruncate_ExactLength(t *testing.T) {
	got := truncate("abc", 3)
	if got != "abc" {
		t.Errorf("truncate(\"abc\", 3) = %q, want %q", got, "abc")
	}
}

func TestTruncate_UTF8(t *testing.T) {
	// 5 runes, each multi-byte
	input := "ééééé" // "eeeee" with accents
	got := truncate(input, 3)
	want := "éé~"
	if got != want {
		t.Errorf("truncate(%q, 3) = %q, want %q", input, got, want)
	}
}

func TestTruncate_Emoji(t *testing.T) {
	input := "\U0001f600\U0001f601\U0001f602\U0001f603" // 4 emoji
	got := truncate(input, 3)
	want := "\U0001f600\U0001f601~"
	if got != want {
		t.Errorf("truncate(emoji, 3) = %q, want %q", got, want)
	}
}

func TestTruncate_ZeroMaxLen(t *testing.T) {
	got := truncate("hello", 0)
	if got != "" {
		t.Errorf("truncate(\"hello\", 0) = %q, want empty", got)
	}
}

func TestTruncate_NegativeMaxLen(t *testing.T) {
	got := truncate("hello", -5)
	if got != "" {
		t.Errorf("truncate(\"hello\", -5) = %q, want empty", got)
	}
}

func TestTruncate_EmptyString(t *testing.T) {
	got := truncate("", 5)
	if got != "" {
		t.Errorf("truncate(\"\", 5) = %q, want empty", got)
	}
}
