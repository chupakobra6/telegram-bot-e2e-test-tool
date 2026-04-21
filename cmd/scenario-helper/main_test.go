package main

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestRunRenderTextCase(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "case.jsonl")
	text := `кефир "до пт" \\ завтра`

	if err := runRenderTextCase([]string{"--output", path, "--text", text}); err != nil {
		t.Fatalf("runRenderTextCase() error = %v", err)
	}

	file, err := os.Open(path)
	if err != nil {
		t.Fatalf("os.Open() error = %v", err)
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	var lines []map[string]any
	for scanner.Scan() {
		var line map[string]any
		if err := json.Unmarshal(scanner.Bytes(), &line); err != nil {
			t.Fatalf("invalid jsonl line: %v", err)
		}
		lines = append(lines, line)
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("scanner.Err() = %v", err)
	}

	if len(lines) != 6 {
		t.Fatalf("len(lines) = %d, want 6", len(lines))
	}
	if got := lines[1]["text"]; got != text {
		t.Fatalf("text = %#v, want %#v", got, text)
	}
	if got := lines[4]["button_text"]; got != "↩️ Отмена" {
		t.Fatalf("button_text = %#v", got)
	}
}

func TestRunRenderTextCaseWithoutCleanup(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "case.jsonl")

	if err := runRenderTextCase([]string{"--output", path, "--text", "кефир", "--cancel-button", ""}); err != nil {
		t.Fatalf("runRenderTextCase() error = %v", err)
	}

	file, err := os.Open(path)
	if err != nil {
		t.Fatalf("os.Open() error = %v", err)
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	count := 0
	for scanner.Scan() {
		count++
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("scanner.Err() = %v", err)
	}
	if count != 4 {
		t.Fatalf("line count = %d, want 4", count)
	}
}
