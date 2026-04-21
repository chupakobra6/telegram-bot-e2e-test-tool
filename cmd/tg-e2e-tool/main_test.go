package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/igor/telegram-bot-e2e-test-tool/internal/config"
	"github.com/igor/telegram-bot-e2e-test-tool/internal/ratesweep"
	"github.com/igor/telegram-bot-e2e-test-tool/internal/transcript"
)

func TestPathModeTreatsDefaultValueAsDefault(t *testing.T) {
	if got := pathMode(".sessions/user.json", ".sessions/user.json"); got != "default(.sessions/user.json)" {
		t.Fatalf("pathMode() = %q", got)
	}
	if got := pathMode("", ".sessions/user.json"); got != "default(.sessions/user.json)" {
		t.Fatalf("pathMode() with empty value = %q", got)
	}
	if got := pathMode("/tmp/custom.json", ".sessions/user.json"); got != "override" {
		t.Fatalf("pathMode() with custom value = %q", got)
	}
}

func TestSaveTranscriptAndArtifactMap(t *testing.T) {
	dir := t.TempDir()
	cfg := config.Config{TranscriptOutputDir: dir}
	tr := transcript.New()

	artifact, err := saveTranscript(cfg, tr, "scenario-a", os.Stderr)
	if err != nil {
		t.Fatalf("saveTranscript() error = %v", err)
	}
	if filepath.Base(artifact.TranscriptJSON) != "scenario-a.json" {
		t.Fatalf("unexpected transcript json path: %s", artifact.TranscriptJSON)
	}
	if filepath.Base(artifact.TranscriptText) != "scenario-a.txt" {
		t.Fatalf("unexpected transcript text path: %s", artifact.TranscriptText)
	}

	artifact.ScenarioPath = "/tmp/scenario-a.jsonl"
	saveLastRunArtifacts(cfg, []runScenarioArtifact{artifact}, os.Stderr)

	body, err := os.ReadFile(filepath.Join(dir, lastRunArtifactsFile))
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}

	var decoded runScenarioArtifactMap
	if err := json.Unmarshal(body, &decoded); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	if len(decoded.Entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(decoded.Entries))
	}
	if decoded.Entries[0].ScenarioPath != "/tmp/scenario-a.jsonl" {
		t.Fatalf("unexpected scenario path: %s", decoded.Entries[0].ScenarioPath)
	}
}

func TestParseRateSweepArgsPrepareScenario(t *testing.T) {
	opts, scenarioPaths, err := parseRateSweepArgs([]string{
		"--chat", "@examplebot",
		"--prepare-scenario", "examples/bench/00-shelfy-home-warmup.jsonl",
		"--prepare-scenario", "custom-reset.jsonl",
		"--runs", "2",
		"bench-a.jsonl",
	})
	if err != nil {
		t.Fatalf("parseRateSweepArgs() error = %v", err)
	}
	if opts.TargetChat != "@examplebot" {
		t.Fatalf("TargetChat = %q", opts.TargetChat)
	}
	if opts.Runs != 2 {
		t.Fatalf("Runs = %d", opts.Runs)
	}
	wantPrepare := []string{"examples/bench/00-shelfy-home-warmup.jsonl", "custom-reset.jsonl"}
	if !reflect.DeepEqual(opts.PreparePaths, wantPrepare) {
		t.Fatalf("PreparePaths = %#v, want %#v", opts.PreparePaths, wantPrepare)
	}
	if !reflect.DeepEqual(scenarioPaths, []string{"bench-a.jsonl"}) {
		t.Fatalf("scenarioPaths = %#v", scenarioPaths)
	}
	if opts.MinActionSpacing != time.Duration(ratesweep.DefaultMinActionSpacingMS)*time.Millisecond {
		t.Fatalf("MinActionSpacing = %s", opts.MinActionSpacing)
	}
}
