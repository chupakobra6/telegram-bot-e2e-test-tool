package ratesweep

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/igor/telegram-bot-e2e-test-tool/internal/mtproto"
)

func TestValidateScenarioInputsRequiresTargetChatForPlaceholder(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "scenario.jsonl")
	body := []byte("{\"id\":\"select\",\"action\":\"select_chat\",\"chat\":\"@your_bot_username\"}\n")
	if err := os.WriteFile(path, body, 0o644); err != nil {
		t.Fatalf("write scenario: %v", err)
	}

	err := validateScenarioInputs([]string{path}, "")
	if err == nil {
		t.Fatal("expected target chat validation error")
	}
}

func TestWriteSummary(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "summary.tsv")
	rows := []SummaryRow{
		{
			ProbeIndex:      2,
			ActionSpacingMS: 2300,
			Scenario:        "examples/bench/03-mixed-text-flow.jsonl",
			Runs:            3,
			ExitCode:        0,
			ElapsedMS:       1234,
			FloodWaits:      1,
			TransportFloods: 0,
			FloodOps:        "click_button",
			Passed:          false,
		},
	}

	if err := writeSummary(path, rows); err != nil {
		t.Fatalf("writeSummary() error = %v", err)
	}

	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read summary: %v", err)
	}
	text := string(body)
	for _, part := range []string{
		"probe\taction_spacing_ms\tscenario\truns\texit_code\telapsed_ms\tflood_waits\ttransport_floods\tflood_ops\tpassed",
		"2\t2300\texamples/bench/03-mixed-text-flow.jsonl\t3\t0\t1234\t1\t0\tclick_button\tfalse",
	} {
		if !strings.Contains(text, part) {
			t.Fatalf("summary missing %q in %q", part, text)
		}
	}
}

func TestBuildCandidates(t *testing.T) {
	got := buildCandidates(1800*time.Millisecond, 2200*time.Millisecond, 200*time.Millisecond)
	want := []time.Duration{
		1800 * time.Millisecond,
		2 * time.Second,
		2200 * time.Millisecond,
	}
	if len(got) != len(want) {
		t.Fatalf("len(candidates) = %d, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("candidate[%d] = %s, want %s", i, got[i], want[i])
		}
	}
}

func TestNormalizeOptionsAddsDefaultPrepareScenarioForBuiltInBench(t *testing.T) {
	got, err := normalizeOptions(Options{})
	if err != nil {
		t.Fatalf("normalizeOptions() error = %v", err)
	}
	if len(got.PreparePaths) != 1 || got.PreparePaths[0] != "examples/bench/00-shelfy-home-warmup.jsonl" {
		t.Fatalf("PreparePaths = %#v", got.PreparePaths)
	}
}

func TestNormalizeOptionsDoesNotForcePrepareScenarioForCustomBench(t *testing.T) {
	got, err := normalizeOptions(Options{
		ScenarioPaths: []string{"custom-bench.jsonl"},
	})
	if err != nil {
		t.Fatalf("normalizeOptions() error = %v", err)
	}
	if len(got.PreparePaths) != 0 {
		t.Fatalf("PreparePaths = %#v, want empty", got.PreparePaths)
	}
}

func TestSummarizeFloodOps(t *testing.T) {
	events := []mtproto.FloodEvent{
		{Operation: "resolve_username"},
		{Operation: "click_button"},
		{Operation: "click_button"},
	}
	got := summarizeFloodOps(events, mtproto.Stats{FloodWaits: 0, TransportFloods: 0}, mtproto.Stats{FloodWaits: 2, TransportFloods: 0})
	if got != "click_button" {
		t.Fatalf("summarizeFloodOps() = %q, want %q", got, "click_button")
	}
}
