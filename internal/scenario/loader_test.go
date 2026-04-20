package scenario

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadScenario(t *testing.T) {
	path := filepath.Join(t.TempDir(), "scenario.jsonl")
	body := []byte("{\"id\":\"start\",\"action\":\"send_text\",\"chat\":\"@bot\",\"text\":\"/start\"}\n{\"id\":\"wait\",\"action\":\"wait\",\"timeout_ms\":1000}\n")
	if err := os.WriteFile(path, body, 0o644); err != nil {
		t.Fatalf("write scenario: %v", err)
	}
	file, err := Load(path)
	if err != nil {
		t.Fatalf("load scenario: %v", err)
	}
	if len(file) != 2 {
		t.Fatalf("expected scenario steps")
	}
	if file[0].Action != "send_text" {
		t.Fatalf("unexpected first action: %s", file[0].Action)
	}
}
