package ratesweep

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/igor/telegram-bot-e2e-test-tool/internal/config"
	"github.com/igor/telegram-bot-e2e-test-tool/internal/engine"
	"github.com/igor/telegram-bot-e2e-test-tool/internal/state"
	"io"
	"os"
	"path/filepath"
	"reflect"
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

type localSession struct {
	engine.Transport
	cooldowns  int
	syncCalls  int
	stats      mtproto.Stats
	events     []mtproto.FloodEvent
	pacing     []time.Duration
	onSync     func(*localSession) error
	onCooldown func(*localSession) error
}

func (s *localSession) SetFloodWaitRetry(bool) {}
func (s *localSession) ConfigurePacing(action, rpc, pinned time.Duration) {
	s.pacing = append(s.pacing, rpc)
}
func (s *localSession) Cooldown(context.Context, time.Duration) error {
	s.cooldowns++
	if s.onCooldown != nil {
		return s.onCooldown(s)
	}
	return nil
}
func (s *localSession) Stats() mtproto.Stats { return s.stats }
func (s *localSession) FloodEvents() []mtproto.FloodEvent {
	return append([]mtproto.FloodEvent{}, s.events...)
}
func (s *localSession) SyncChat(context.Context, string, int, int) (state.ChatState, error) {
	s.syncCalls++
	if s.onSync != nil {
		if err := s.onSync(s); err != nil {
			return state.ChatState{}, err
		}
	}
	return state.ChatState{Messages: []state.VisibleMessage{{ID: 1, Text: "ready", Sender: "bot"}}}, nil
}
func benchmarkFile(t *testing.T, name, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte(body), 0600); err != nil {
		t.Fatal(err)
	}
	return path
}

const localBenchmark = "{\"action\":\"assert_visible_text\",\"chat\":\"@fake\",\"text\":\"ready\",\"timeout_ms\":1}\n"

func testOptions(t *testing.T) Options {
	return Options{ScenarioPaths: []string{benchmarkFile(t, "bench.jsonl", localBenchmark)}, Runs: 1, ArtifactRoot: t.TempDir(), MinActionSpacing: time.Millisecond, MaxActionSpacing: 3 * time.Millisecond, Resolution: time.Millisecond, ProbeSyncInterval: time.Millisecond}
}
func fakeAuthorize(s *localSession) authorizeFunc {
	return func(ctx context.Context, fn func(context.Context, probeSession) error) error { return fn(ctx, s) }
}

func TestSearchOnlyUsesValidRateObservationsAndConfirms(t *testing.T) {
	for _, outcome := range []Outcome{ProductFailed, Unavailable} {
		t.Run(string(outcome), func(t *testing.T) {
			calls := 0
			opts := testOptions(t)
			rec, err := search(opts, Recommendation{}, func(spacing time.Duration, index int, confirm bool) (ProbeResult, error) {
				calls++
				if calls == 1 {
					return ProbeResult{Outcome: Passed}, nil
				}
				return ProbeResult{Outcome: outcome, Reason: "not a speed result"}, nil
			})
			if err == nil || rec.Found || rec.Confirmed || calls != 2 {
				t.Fatalf("rec=%+v err=%v calls=%d", rec, err, calls)
			}
		})
	}
	opts := testOptions(t)
	var spacings []int64
	confirmations := 0
	rec, err := search(opts, Recommendation{}, func(spacing time.Duration, index int, confirm bool) (ProbeResult, error) {
		spacings = append(spacings, spacing.Milliseconds())
		if confirm {
			confirmations++
		}
		outcome := Passed
		if spacing < 3*time.Millisecond {
			outcome = RateLimited
		}
		return ProbeResult{Outcome: outcome}, nil
	})
	if err != nil || !rec.Confirmed || rec.ActionSpacingMS != 3 || confirmations != 1 || !reflect.DeepEqual(spacings, []int64{2, 3, 3}) {
		t.Fatalf("rec=%+v err=%v spacings=%v confirmations=%d", rec, err, spacings, confirmations)
	}
}

func TestClassifyProbeCause(t *testing.T) {
	for _, test := range []struct {
		name             string
		row              SummaryRow
		execErr, errorIO error
		want             Outcome
	}{
		{name: "complete", row: SummaryRow{Runs: 2, CompletedRuns: 2}, want: Passed},
		{name: "incomplete", row: SummaryRow{Runs: 2, CompletedRuns: 1}, want: Unavailable},
		{name: "action flood", row: SummaryRow{FloodWaits: 1, FloodOps: "send_text"}, want: RateLimited},
		{name: "RPC flood", row: SummaryRow{FloodWaits: 1, FloodOps: "get_history"}, want: Unavailable},
		{name: "transport flood", row: SummaryRow{TransportFloods: 1}, want: Unavailable},
		{name: "product", execErr: &engine.ProductFailure{Err: errors.New("not visible")}, want: ProductFailed},
		{name: "cancel", execErr: context.Canceled, want: Unavailable},
		{name: "save", row: SummaryRow{FloodWaits: 1, FloodOps: "send_text"}, errorIO: errors.New("disk unavailable"), want: Unavailable},
	} {
		t.Run(test.name, func(t *testing.T) {
			got, reason := classify(test.row, test.execErr, test.errorIO)
			if got != test.want || reason == "" {
				t.Fatalf("%s %s", got, reason)
			}
		})
	}
}

func TestPreparationFailureCannotBecomeRateLimit(t *testing.T) {
	opts := testOptions(t)
	opts.PreparePaths = opts.ScenarioPaths
	session := &localSession{onSync: func(s *localSession) error {
		s.stats.FloodWaits++
		s.events = append(s.events, mtproto.FloodEvent{Operation: "send_text"})
		return errors.New("flood wait")
	}}
	result, err := runProbe(context.Background(), session, config.Config{SyncInterval: time.Millisecond}, opts, t.TempDir(), time.Millisecond, 1)
	if err != nil || result.Outcome != Unavailable || len(result.ScenarioRows) != 1 || result.ScenarioRows[0].Phase != "prepare" || result.Passed {
		t.Fatalf("%+v %v", result, err)
	}
	if session.cooldowns != 1 {
		t.Fatal("measurement continued after preparation failed")
	}
}

func TestSweepPersistsCompletedProbesOnLaterFailure(t *testing.T) {
	for _, writeFailure := range []bool{false, true} {
		t.Run(fmt.Sprint(writeFailure), func(t *testing.T) {
			opts := testOptions(t)
			session := &localSession{onSync: func(s *localSession) error {
				if s.cooldowns < 2 {
					return nil
				}
				if !writeFailure {
					return context.Canceled
				}
				matches, err := filepath.Glob(filepath.Join(opts.ArtifactRoot, "*", "summary.tsv"))
				if err != nil || len(matches) != 1 {
					t.Fatalf("summary not initialized: %v %v", matches, err)
				}
				if err := os.Remove(matches[0]); err != nil {
					t.Fatal(err)
				}
				if err := os.Mkdir(matches[0], 0700); err != nil {
					t.Fatal(err)
				}
				return nil
			}}
			err := run(context.Background(), config.Config{SyncInterval: time.Millisecond, RPCSpacing: 2 * time.Second}, io.Discard, opts, fakeAuthorize(session))
			if err == nil {
				t.Fatal("failure returned success")
			}
			files, _ := filepath.Glob(filepath.Join(opts.ArtifactRoot, "*", "probes.json"))
			if len(files) != 1 {
				t.Fatal(files)
			}
			body, err := os.ReadFile(files[0])
			if err != nil {
				t.Fatal(err)
			}
			var report struct {
				Probes         []ProbeResult
				Recommendation Recommendation
			}
			if err := json.Unmarshal(body, &report); err != nil {
				t.Fatal(err)
			}
			if len(report.Probes) != 2 || report.Probes[0].Outcome != Passed || report.Recommendation.Found || report.Recommendation.Confirmed || report.Recommendation.Error == "" {
				t.Fatalf("%+v", report)
			}
			for _, rpc := range session.pacing {
				if rpc < 2*time.Second {
					t.Fatal("configured RPC floor reduced")
				}
			}
			recBody, err := os.ReadFile(filepath.Join(filepath.Dir(files[0]), "recommendation.txt"))
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(string(recBody), "found=false") {
				t.Fatal(string(recBody))
			}
		})
	}
}

func TestSweepConfirmationRepeatsComparableWork(t *testing.T) {
	opts := testOptions(t)
	opts.Runs = 2
	opts.PreparePaths = []string{benchmarkFile(t, "prepare.jsonl", localBenchmark)}
	session := &localSession{}
	if err := run(context.Background(), config.Config{SyncInterval: time.Millisecond}, io.Discard, opts, fakeAuthorize(session)); err != nil {
		t.Fatal(err)
	}
	files, _ := filepath.Glob(filepath.Join(opts.ArtifactRoot, "*", "probes.json"))
	body, _ := os.ReadFile(files[0])
	var report struct {
		Probes         []ProbeResult
		Recommendation Recommendation
	}
	if err := json.Unmarshal(body, &report); err != nil {
		t.Fatal(err)
	}
	if len(report.Probes) != 3 || !report.Probes[2].Confirmation || !report.Recommendation.Confirmed || report.Recommendation.ProbeIndex != 3 {
		t.Fatalf("%+v", report)
	}
	for _, probe := range report.Probes {
		if len(probe.ScenarioRows) != 2 || probe.ScenarioRows[0].CompletedRuns != 1 || probe.ScenarioRows[1].CompletedRuns != 2 || probe.InputDigest != report.Probes[0].InputDigest {
			t.Fatalf("uncomparable probe: %+v", probe)
		}
	}
	if session.syncCalls != 9 {
		t.Fatalf("expected prep+2 repeats for every probe, got %d", session.syncCalls)
	}
}

func TestProbeKeepsCompletedRowsWhenTranscriptSaveFails(t *testing.T) {
	opts := testOptions(t)
	opts.ScenarioPaths = append(opts.ScenarioPaths, benchmarkFile(t, "next.jsonl", localBenchmark))
	dir := t.TempDir()
	session := &localSession{onSync: func(s *localSession) error {
		if s.syncCalls == 2 {
			path := filepath.Join(dir, "transcripts", "01-1ms", "measure", "next__run-01.txt")
			if err := os.Mkdir(path, 0700); err != nil {
				t.Fatal(err)
			}
		}
		return nil
	}}
	result, err := runProbe(context.Background(), session, config.Config{SyncInterval: time.Millisecond}, opts, dir, time.Millisecond, 1)
	if err == nil || result.Outcome != Unavailable || len(result.ScenarioRows) != 2 || !result.ScenarioRows[0].Passed || result.ScenarioRows[1].CompletedRuns != 1 {
		t.Fatalf("%+v %v", result, err)
	}
	if _, err := os.Stat(filepath.Join(dir, "transcripts", "01-1ms", "measure", "next__run-01.json")); err != nil {
		t.Fatal("partial result lost", err)
	}
}

func TestSweepInvalidatesRecommendationWhenLastFileCannotBeWritten(t *testing.T) {
	opts := testOptions(t)
	opts.MinActionSpacing = opts.MaxActionSpacing
	session := &localSession{onSync: func(s *localSession) error {
		// One search probe, then confirmation. Break the final view after work succeeds.
		if s.syncCalls == 2 {
			paths, _ := filepath.Glob(filepath.Join(opts.ArtifactRoot, "*", "recommendation.txt"))
			if len(paths) != 1 {
				t.Fatal(paths)
			}
			if err := os.Remove(paths[0]); err != nil {
				t.Fatal(err)
			}
			if err := os.Mkdir(paths[0], 0700); err != nil {
				t.Fatal(err)
			}
		}
		return nil
	}}
	if err := run(context.Background(), config.Config{SyncInterval: time.Millisecond}, io.Discard, opts, fakeAuthorize(session)); err == nil {
		t.Fatal("last file error ignored")
	}
	files, _ := filepath.Glob(filepath.Join(opts.ArtifactRoot, "*", "probes.json"))
	body, _ := os.ReadFile(files[0])
	var report struct {
		Probes         []ProbeResult
		Recommendation Recommendation
	}
	if err := json.Unmarshal(body, &report); err != nil {
		t.Fatal(err)
	}
	if len(report.Probes) != 2 || report.Recommendation.Found || report.Recommendation.Confirmed || report.Recommendation.Error == "" {
		t.Fatalf("inconsistent successful view: %+v", report)
	}
}

func TestSweepDetectsInputsChangedDuringConfirmation(t *testing.T) {
	opts := testOptions(t)
	opts.MinActionSpacing = opts.MaxActionSpacing
	session := &localSession{onSync: func(s *localSession) error {
		if s.syncCalls == 2 {
			if err := os.WriteFile(opts.ScenarioPaths[0], []byte(localBenchmark+"# changed during confirmation\n"), 0600); err != nil {
				t.Fatal(err)
			}
		}
		return nil
	}}
	err := run(context.Background(), config.Config{SyncInterval: time.Millisecond}, io.Discard, opts, fakeAuthorize(session))
	if err == nil || !strings.Contains(err.Error(), "inputs changed during probe") {
		t.Fatalf("input change missed: %v", err)
	}
	files, _ := filepath.Glob(filepath.Join(opts.ArtifactRoot, "*", "probes.json"))
	body, _ := os.ReadFile(files[0])
	var report struct {
		Probes         []ProbeResult
		Recommendation Recommendation
	}
	if err := json.Unmarshal(body, &report); err != nil {
		t.Fatal(err)
	}
	if report.Recommendation.Confirmed || len(report.Probes) != 2 || report.Probes[1].Outcome != Unavailable {
		t.Fatalf("%+v", report)
	}
}

func TestSweepRetainsProbesWhenAuthorizationSessionCleanupFails(t *testing.T) {
	opts := testOptions(t)
	opts.MinActionSpacing = opts.MaxActionSpacing
	session := &localSession{}
	err := run(context.Background(), config.Config{SyncInterval: time.Millisecond}, io.Discard, opts, func(ctx context.Context, fn func(context.Context, probeSession) error) error {
		if err := fn(ctx, session); err != nil {
			return err
		}
		return errors.New("session cleanup failed")
	})
	if err == nil {
		t.Fatal("session cleanup ignored")
	}
	files, _ := filepath.Glob(filepath.Join(opts.ArtifactRoot, "*", "probes.json"))
	body, _ := os.ReadFile(files[0])
	var report struct {
		Probes         []ProbeResult
		Recommendation Recommendation
	}
	if err := json.Unmarshal(body, &report); err != nil {
		t.Fatal(err)
	}
	if len(report.Probes) != 2 || report.Recommendation.Confirmed || !strings.Contains(report.Recommendation.Error, "cleanup") {
		t.Fatalf("%+v", report)
	}
}
