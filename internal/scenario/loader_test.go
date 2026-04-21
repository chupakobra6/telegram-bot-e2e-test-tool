package scenario

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/igor/telegram-bot-e2e-test-tool/internal/protocol"
)

func TestLoadScenario(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "scenario.jsonl")
	body := []byte("{\"id\":\"start\",\"action\":\"send_text\",\"chat\":\"@bot\",\"text\":\"/start\"}\n{\"id\":\"wait\",\"action\":\"wait\",\"timeout_ms\":1000}\n")
	if err := os.WriteFile(path, body, 0o644); err != nil {
		t.Fatalf("write scenario: %v", err)
	}
	var got []struct {
		id     string
		action string
	}
	if err := Read(path, func(cmd protocol.Command) error {
		got = append(got, struct {
			id     string
			action string
		}{id: cmd.ID, action: cmd.Action})
		return nil
	}); err != nil {
		t.Fatalf("read scenario: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("expected scenario steps")
	}
	if got[0].action != "send_text" {
		t.Fatalf("unexpected first action: %s", got[0].action)
	}
}

func TestLoadScenarioResolvesRelativeMediaPath(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "scenario.jsonl")
	body := []byte("{\"id\":\"doc\",\"action\":\"send_document\",\"path\":\"fixtures/sample.txt\"}\n")
	if err := os.WriteFile(path, body, 0o644); err != nil {
		t.Fatalf("write scenario: %v", err)
	}
	want := filepath.Join(dir, "fixtures", "sample.txt")
	var got protocol.Command
	if err := Read(path, func(cmd protocol.Command) error {
		got = cmd
		return nil
	}); err != nil {
		t.Fatalf("read scenario: %v", err)
	}
	if got.Path != want {
		t.Fatalf("expected resolved path %q, got %q", want, got.Path)
	}
}

func TestReadWithOptionsReplacesChatPlaceholder(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "scenario.jsonl")
	body := []byte("{\"id\":\"select\",\"action\":\"select_chat\",\"chat\":\"@your_bot_username\"}\n")
	if err := os.WriteFile(path, body, 0o644); err != nil {
		t.Fatalf("write scenario: %v", err)
	}

	var got protocol.Command
	if err := ReadWithOptions(path, ReadOptions{TargetChat: "@real_bot"}, func(cmd protocol.Command) error {
		got = cmd
		return nil
	}); err != nil {
		t.Fatalf("read scenario: %v", err)
	}
	if got.Chat != "@real_bot" {
		t.Fatalf("expected placeholder replacement, got %q", got.Chat)
	}
}

func TestReadWithOptionsRequiresTargetChatForPlaceholder(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "scenario.jsonl")
	body := []byte("{\"id\":\"select\",\"action\":\"select_chat\",\"chat\":\"@your_bot_username\"}\n")
	if err := os.WriteFile(path, body, 0o644); err != nil {
		t.Fatalf("write scenario: %v", err)
	}

	err := ReadWithOptions(path, ReadOptions{}, func(cmd protocol.Command) error {
		return nil
	})
	if err == nil {
		t.Fatal("expected placeholder error")
	}
}

func TestReadWithOptionsResolvesFixturePlaceholder(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "scenario.jsonl")
	body := []byte("{\"id\":\"photo\",\"action\":\"send_photo\",\"path\":\"@fixtures/e2e-photo.png\"}\n")
	if err := os.WriteFile(path, body, 0o644); err != nil {
		t.Fatalf("write scenario: %v", err)
	}

	var got protocol.Command
	if err := ReadWithOptions(path, ReadOptions{FixtureDir: "/tmp/fixtures"}, func(cmd protocol.Command) error {
		got = cmd
		return nil
	}); err != nil {
		t.Fatalf("read scenario: %v", err)
	}
	if got.Path != filepath.Join("/tmp/fixtures", "e2e-photo.png") {
		t.Fatalf("expected fixture path to be resolved, got %q", got.Path)
	}
}

func TestUsesChatPlaceholder(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "scenario.jsonl")
	body := []byte("{\"id\":\"select\",\"action\":\"select_chat\",\"chat\":\"@your_bot_username\"}\n")
	if err := os.WriteFile(path, body, 0o644); err != nil {
		t.Fatalf("write scenario: %v", err)
	}

	uses, err := UsesChatPlaceholder(path)
	if err != nil {
		t.Fatalf("UsesChatPlaceholder() error = %v", err)
	}
	if !uses {
		t.Fatal("expected placeholder to be detected")
	}
}
