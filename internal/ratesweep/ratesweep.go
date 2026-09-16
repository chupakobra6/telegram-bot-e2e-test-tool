package ratesweep

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/igor/telegram-bot-e2e-test-tool/internal/config"
	"github.com/igor/telegram-bot-e2e-test-tool/internal/engine"
	"github.com/igor/telegram-bot-e2e-test-tool/internal/mtproto"
	"github.com/igor/telegram-bot-e2e-test-tool/internal/protocol"
	"github.com/igor/telegram-bot-e2e-test-tool/internal/scenario"
	"github.com/igor/telegram-bot-e2e-test-tool/internal/transcript"
)

const (
	DefaultArtifactRoot          = "artifacts/rate-sweep"
	DefaultRuns                  = 3
	DefaultMinActionSpacingMS    = 1800
	DefaultMaxActionSpacingMS    = 3000
	DefaultResolutionMS          = 100
	DefaultProbeSyncIntervalMS   = 1600
	DefaultProbeRPCSpacingMS     = 700
	DefaultProbePinnedCacheTTLMS = 45000
	DefaultProbeCooldown         = 5 * time.Second
)

var DefaultScenarioPaths = []string{
	"examples/bench/01-read-heavy-dump-state.jsonl",
	"examples/bench/02-write-heavy-settings.jsonl",
	"examples/bench/03-mixed-text-flow.jsonl",
}

var DefaultPrepareScenarioPaths = []string{
	"examples/bench/00-shelfy-home-warmup.jsonl",
}

type Options struct {
	ScenarioPaths     []string
	PreparePaths      []string
	Runs              int
	ArtifactRoot      string
	TargetChat        string
	MinActionSpacing  time.Duration
	MaxActionSpacing  time.Duration
	Resolution        time.Duration
	ProbeSyncInterval time.Duration
	ProbeRPCSpacing   time.Duration
	ProbePinnedTTL    time.Duration
	Now               func() time.Time
}

type Outcome string

const (
	Passed        Outcome = "passed"
	RateLimited   Outcome = "rate_limit"
	ProductFailed Outcome = "product_failure"
	Unavailable   Outcome = "unavailable"
)

type ProbeResult struct {
	Outcome         Outcome
	Confirmation    bool
	InputDigest     string
	ProbeIndex      int
	ActionSpacingMS int64
	Passed          bool
	Reason          string
	ScenarioRows    []SummaryRow
	TotalElapsedMS  int64
	FloodWaits      int
	TransportFloods int
}

type SummaryRow struct {
	Phase           string
	Outcome         Outcome
	Reason          string
	CompletedRuns   int
	ProbeIndex      int
	ActionSpacingMS int64
	Scenario        string
	Runs            int
	ExitCode        int
	ElapsedMS       int64
	FloodWaits      int
	TransportFloods int
	FloodOps        string
	Passed          bool
}

type Recommendation struct {
	Confirmed       bool
	ProbeIndex      int
	Error           string
	Found           bool
	LimitReached    bool
	ActionSpacingMS int64
	SyncIntervalMS  int64
	RPCSpacingMS    int64
	PinnedTTLMS     int64
}

type probeSession interface {
	engine.Transport
	SetFloodWaitRetry(bool)
	ConfigurePacing(time.Duration, time.Duration, time.Duration)
	Cooldown(context.Context, time.Duration) error
	Stats() mtproto.Stats
	FloodEvents() []mtproto.FloodEvent
}

type authorizeFunc func(context.Context, func(context.Context, probeSession) error) error

func Run(ctx context.Context, baseCfg config.Config, stdout io.Writer, opts Options) error {
	client := mtproto.New(baseCfg)
	return run(ctx, baseCfg, stdout, opts, func(ctx context.Context, fn func(context.Context, probeSession) error) error {
		return client.RunAuthorized(ctx, func(ctx context.Context, session *mtproto.Session) error { return fn(ctx, session) })
	})
}

func run(ctx context.Context, baseCfg config.Config, stdout io.Writer, opts Options, authorize authorizeFunc) error {
	if stdout == nil {
		stdout = io.Discard
	}
	normalized, err := normalizeOptions(opts)
	if err != nil {
		return err
	}
	// Keep the configured RPC floor: action cadence is the searched variable.
	if normalized.ProbeRPCSpacing < baseCfg.RPCSpacing {
		normalized.ProbeRPCSpacing = baseCfg.RPCSpacing
	}
	if err := os.MkdirAll(normalized.ArtifactRoot, 0o755); err != nil {
		return err
	}
	outDir, err := os.MkdirTemp(normalized.ArtifactRoot, normalized.Now().UTC().Format("20060102T150405Z")+"-")
	if err != nil {
		return fmt.Errorf("create rate sweep directory: %w", err)
	}
	fmt.Fprintf(stdout, "rate sweep artifacts: %s\n", outDir)
	var results []ProbeResult
	rows := []SummaryRow{}
	rec := Recommendation{SyncIntervalMS: normalized.ProbeSyncInterval.Milliseconds(), RPCSpacingMS: normalized.ProbeRPCSpacing.Milliseconds(), PinnedTTLMS: normalized.ProbePinnedTTL.Milliseconds()}
	save := func() error {
		summaryErr := writeSummary(filepath.Join(outDir, "summary.tsv"), rows)
		if summaryErr != nil {
			rec.Found, rec.Confirmed, rec.ActionSpacingMS = false, false, 0
			rec.Error = summaryErr.Error()
		}
		body, marshalErr := json.MarshalIndent(struct {
			Options        map[string]any
			Probes         []ProbeResult
			Recommendation Recommendation
		}{Options: normalizedForReport(normalized), Probes: results, Recommendation: rec}, "", "  ")
		if marshalErr == nil {
			marshalErr = writeAtomic(filepath.Join(outDir, "probes.json"), body)
		}
		if marshalErr != nil {
			rec.Found, rec.Confirmed, rec.ActionSpacingMS = false, false, 0
			rec.Error = marshalErr.Error()
		}
		return errors.Join(summaryErr, marshalErr, writeRecommendation(filepath.Join(outDir, "recommendation.txt"), rec, normalized))
	}

	// The report exists before validation and authorization, including early failures.
	runErr := save()
	if runErr == nil {
		runErr = validateScenarioInputs(normalized.ScenarioPaths, normalized.TargetChat)
	}
	if runErr == nil {
		runErr = validateScenarioInputs(normalized.PreparePaths, normalized.TargetChat)
	}
	var digest string
	if runErr == nil {
		digest, runErr = inputDigest(normalized)
	}
	if runErr == nil {
		runErr = authorize(ctx, func(ctx context.Context, session probeSession) error {
			session.SetFloodWaitRetry(false)
			var searchErr error
			rec, searchErr = search(normalized, rec, func(spacing time.Duration, index int, confirmation bool) (ProbeResult, error) {
				currentDigest, err := inputDigest(normalized)
				result := ProbeResult{ProbeIndex: index, ActionSpacingMS: spacing.Milliseconds(), Outcome: Unavailable, Reason: "scenario inputs changed during sweep", InputDigest: currentDigest, Confirmation: confirmation}
				if err == nil && currentDigest == digest {
					result, err = runProbe(ctx, session, baseCfg, normalized, outDir, spacing, index)
					result.InputDigest, result.Confirmation = digest, confirmation
					postDigest, postErr := inputDigest(normalized)
					if postErr != nil {
						err = errors.Join(err, postErr)
					} else if postDigest != digest {
						err = errors.Join(err, fmt.Errorf("scenario inputs changed during probe"))
					}
				} else if err == nil {
					err = fmt.Errorf("scenario inputs changed during sweep")
				}
				if err != nil {
					result.Outcome, result.Passed, result.Reason = Unavailable, false, err.Error()
				}
				results = append(results, result)
				rows = append(rows, result.ScenarioRows...)
				fmt.Fprintf(stdout, "probe=%d action=%dms outcome=%s confirmation=%t reason=%s\n", index, spacing.Milliseconds(), result.Outcome, confirmation, result.Reason)
				return result, errors.Join(err, save())
			})
			return searchErr
		})
	}
	if runErr != nil {
		rec.Found, rec.Confirmed = false, false
		rec.ActionSpacingMS = 0
		rec.Error = runErr.Error()
	}
	saveErr := save()
	if saveErr != nil {
		rec.Found, rec.Confirmed, rec.ActionSpacingMS = false, false, 0
		rec.Error = errors.Join(runErr, saveErr).Error()
		// A report write failure must not leave a published successful recommendation.
		saveErr = errors.Join(saveErr, save())
	}
	renderSummaryTable(stdout, rows)
	if runErr == nil && saveErr == nil {
		renderRecommendation(stdout, rec, normalized)
	}
	return errors.Join(runErr, saveErr)
}

// Function values cannot be encoded; the clock is execution-only, not a probe input.
func normalizedForReport(opts Options) map[string]any {
	return map[string]any{"scenarios": opts.ScenarioPaths, "prepare": opts.PreparePaths, "runs": opts.Runs,
		"target_chat": opts.TargetChat, "min_action_ms": opts.MinActionSpacing.Milliseconds(), "max_action_ms": opts.MaxActionSpacing.Milliseconds(),
		"resolution_ms": opts.Resolution.Milliseconds(), "rpc_spacing_ms": opts.ProbeRPCSpacing.Milliseconds(), "sync_interval_ms": opts.ProbeSyncInterval.Milliseconds(), "pinned_ttl_ms": opts.ProbePinnedTTL.Milliseconds()}
}

func inputDigest(opts Options) (string, error) {
	h := sha256.New()
	for _, path := range append(append([]string{}, opts.PreparePaths...), opts.ScenarioPaths...) {
		body, err := os.ReadFile(path)
		if err != nil {
			return "", err
		}
		fmt.Fprintf(h, "%d:%s:%d:", len(path), path, len(body))
		h.Write(body)
	}
	return fmt.Sprintf("%x", h.Sum(nil)), nil
}

func search(opts Options, rec Recommendation, probe func(time.Duration, int, bool) (ProbeResult, error)) (Recommendation, error) {
	candidates := buildCandidates(opts.MinActionSpacing, opts.MaxActionSpacing, opts.Resolution)
	lo, hi, best, order := 0, len(candidates)-1, -1, 0
	for lo <= hi {
		mid := lo + (hi-lo)/2
		order++
		result, err := probe(candidates[mid], order, false)
		if err != nil {
			return rec, err
		}
		switch result.Outcome {
		case Passed:
			best, hi = mid, mid-1
		case RateLimited:
			lo = mid + 1
		default:
			return rec, fmt.Errorf("probe %d %s: %s", order, result.Outcome, result.Reason)
		}
	}
	if best < 0 {
		return rec, fmt.Errorf("no passing action spacing in tested range")
	}
	// Repeat all preparation and useful work at the selected interval.
	order++
	confirmation, err := probe(candidates[best], order, true)
	if err != nil {
		return rec, err
	}
	if confirmation.Outcome != Passed {
		return rec, fmt.Errorf("confirmation %s: %s", confirmation.Outcome, confirmation.Reason)
	}
	rec.Found, rec.Confirmed, rec.ProbeIndex = true, true, order
	rec.ActionSpacingMS, rec.LimitReached = candidates[best].Milliseconds(), best != 0
	return rec, nil
}

func normalizeOptions(opts Options) (Options, error) {
	if opts.Now == nil {
		opts.Now = time.Now
	}
	useBuiltInBench := len(opts.ScenarioPaths) == 0
	if len(opts.ScenarioPaths) == 0 {
		opts.ScenarioPaths = append([]string(nil), DefaultScenarioPaths...)
	}
	if useBuiltInBench && len(opts.PreparePaths) == 0 {
		opts.PreparePaths = append([]string(nil), DefaultPrepareScenarioPaths...)
	}
	if opts.Runs <= 0 {
		opts.Runs = DefaultRuns
	}
	if strings.TrimSpace(opts.ArtifactRoot) == "" {
		opts.ArtifactRoot = DefaultArtifactRoot
	}
	if opts.MinActionSpacing <= 0 {
		opts.MinActionSpacing = time.Duration(DefaultMinActionSpacingMS) * time.Millisecond
	}
	if opts.MaxActionSpacing <= 0 {
		opts.MaxActionSpacing = time.Duration(DefaultMaxActionSpacingMS) * time.Millisecond
	}
	if opts.Resolution <= 0 {
		opts.Resolution = time.Duration(DefaultResolutionMS) * time.Millisecond
	}
	if opts.ProbeSyncInterval <= 0 {
		opts.ProbeSyncInterval = time.Duration(DefaultProbeSyncIntervalMS) * time.Millisecond
	}
	if opts.ProbeRPCSpacing <= 0 {
		opts.ProbeRPCSpacing = time.Duration(DefaultProbeRPCSpacingMS) * time.Millisecond
	}
	if opts.ProbePinnedTTL <= 0 {
		opts.ProbePinnedTTL = time.Duration(DefaultProbePinnedCacheTTLMS) * time.Millisecond
	}
	if opts.MinActionSpacing > opts.MaxActionSpacing {
		return Options{}, fmt.Errorf("min action spacing must be <= max action spacing")
	}
	return opts, nil
}

func buildCandidates(minAction time.Duration, maxAction time.Duration, resolution time.Duration) []time.Duration {
	minMS := minAction.Milliseconds()
	maxMS := maxAction.Milliseconds()
	stepMS := resolution.Milliseconds()
	if stepMS <= 0 {
		stepMS = 100
	}

	candidates := make([]time.Duration, 0, int((maxMS-minMS)/stepMS)+1)
	for ms := minMS; ms <= maxMS; ms += stepMS {
		candidates = append(candidates, time.Duration(ms)*time.Millisecond)
	}
	if len(candidates) == 0 || candidates[len(candidates)-1] != maxAction {
		candidates = append(candidates, maxAction)
	}
	return candidates
}

func validateScenarioInputs(paths []string, targetChat string) error {
	for _, path := range paths {
		if _, err := os.Stat(path); err != nil {
			return fmt.Errorf("missing scenario: %s: %w", path, err)
		}
		usesPlaceholder, err := scenario.UsesChatPlaceholder(path)
		if err != nil {
			return err
		}
		if usesPlaceholder && strings.TrimSpace(targetChat) == "" {
			return fmt.Errorf("scenario %s uses %s; set CHAT=@your_bot_username or pass --chat", path, scenario.DefaultChatPlaceholder)
		}
	}
	return nil
}

func runProbe(ctx context.Context, session probeSession, baseCfg config.Config, opts Options, outDir string, actionSpacing time.Duration, probeIndex int) (result ProbeResult, resultErr error) {
	result = ProbeResult{ProbeIndex: probeIndex, ActionSpacingMS: actionSpacing.Milliseconds(), Outcome: Unavailable}
	start := time.Now()
	defer func() {
		result.TotalElapsedMS = time.Since(start).Milliseconds()
		if resultErr != nil {
			result.Outcome, result.Reason = Unavailable, resultErr.Error()
		}
		result.Passed = result.Outcome == Passed
	}()
	dir := filepath.Join(outDir, "transcripts", fmt.Sprintf("%02d-%dms", probeIndex, actionSpacing.Milliseconds()))
	readOpts := scenario.ReadOptions{TargetChat: opts.TargetChat}
	runBatch := func(path, phase string, syncInterval time.Duration, runs int) (SummaryRow, error) {
		row, err := runScenarioBatch(ctx, session, baseCfg, actionSpacing, probeIndex, filepath.Join(dir, phase), path, readOpts, syncInterval, runs)
		row.Phase = phase
		if phase == "prepare" && row.Outcome != Passed {
			row.Outcome, row.Passed = Unavailable, false
			row.Reason = "preparation failed: " + row.Reason
		}
		result.ScenarioRows = append(result.ScenarioRows, row)
		result.FloodWaits += row.FloodWaits
		result.TransportFloods += row.TransportFloods
		return row, err
	}
	if len(opts.PreparePaths) > 0 {
		if err := session.Cooldown(ctx, DefaultProbeCooldown); err != nil {
			return result, err
		}
		session.ConfigurePacing(baseCfg.ActionSpacing, baseCfg.RPCSpacing, baseCfg.PinnedCacheTTL)
		for _, path := range opts.PreparePaths {
			row, err := runBatch(path, "prepare", baseCfg.SyncInterval, 1)
			if err != nil {
				return result, err
			}
			if row.Outcome != Passed {
				result.Outcome, result.Reason = Unavailable, row.Reason
				return result, nil
			}
		}
	}
	if err := session.Cooldown(ctx, DefaultProbeCooldown); err != nil {
		return result, err
	}
	session.ConfigurePacing(actionSpacing, opts.ProbeRPCSpacing, opts.ProbePinnedTTL)
	for _, path := range opts.ScenarioPaths {
		row, err := runBatch(path, "measure", opts.ProbeSyncInterval, opts.Runs)
		if err != nil {
			return result, err
		}
		if row.Outcome != Passed {
			result.Outcome, result.Reason = row.Outcome, row.Reason
			return result, nil
		}
	}
	result.Outcome, result.Reason = Passed, "all requested runs completed"
	return result, nil
}

func runScenarioBatch(ctx context.Context, session probeSession, baseCfg config.Config, actionSpacing time.Duration, probeIndex int, transcriptDir, scenarioPath string, readOpts scenario.ReadOptions, syncInterval time.Duration, runs int) (row SummaryRow, resultErr error) {
	row = SummaryRow{ProbeIndex: probeIndex, ActionSpacingMS: actionSpacing.Milliseconds(), Scenario: scenarioPath, Runs: runs, Outcome: Unavailable}
	before := session.Stats()
	start := time.Now()
	var executionErr error
	defer func() {
		after := session.Stats()
		row.ElapsedMS = time.Since(start).Milliseconds()
		row.FloodWaits, row.TransportFloods = after.FloodWaits-before.FloodWaits, after.TransportFloods-before.TransportFloods
		row.FloodOps = summarizeFloodOps(session.FloodEvents(), before, after)
		row.Outcome, row.Reason = classify(row, executionErr, resultErr)
		row.Passed = row.Outcome == Passed
		if !row.Passed {
			row.ExitCode = 1
		}
	}()
	baseName := strings.TrimSuffix(filepath.Base(scenarioPath), filepath.Ext(scenarioPath))
	for runIndex := 0; runIndex < runs; runIndex++ {
		prefix := fmt.Sprintf("%s__run-%02d", baseName, runIndex+1)
		logPath := filepath.Join(transcriptDir, prefix+".log")
		if err := os.MkdirAll(filepath.Dir(logPath), 0o755); err != nil {
			return row, err
		}
		logFile, err := os.Create(logPath)
		if err != nil {
			return row, err
		}
		beforeRun := session.Stats()
		tr := transcript.New()
		runner := engine.New(session, tr, logFile, baseCfg.HistoryWindow, syncInterval)
		var actionErr error
		executionErr = scenario.ReadWithOptions(scenarioPath, readOpts, func(cmd protocol.Command) error {
			traceCommand(logFile, "start", cmd, nil)
			err := runner.Execute(ctx, cmd)
			actionErr = err
			if err != nil {
				traceCommand(logFile, "error", cmd, err)
				return err
			}
			traceCommand(logFile, "done", cmd, nil)
			return nil
		})
		closeErr := logFile.Close()
		events := session.FloodEvents()
		afterRun := session.Stats()
		count := afterRun.FloodWaits - beforeRun.FloodWaits + afterRun.TransportFloods - beforeRun.TransportFloods
		if count < len(events) {
			events = events[len(events)-count:]
		}
		floodErr := appendFloodEvents(logPath, events)
		saveErr := saveTranscriptSet(transcriptDir, prefix, tr)
		if executionErr == nil {
			row.CompletedRuns++
		}
		var readErr error
		if executionErr != nil && actionErr == nil {
			readErr = executionErr
		}
		if err := errors.Join(readErr, closeErr, floodErr, saveErr); err != nil {
			return row, err
		}
		if executionErr != nil {
			break
		}
	}
	return row, nil
}

func classify(row SummaryRow, executionErr, infrastructureErr error) (Outcome, string) {
	if infrastructureErr != nil {
		return Unavailable, errors.Join(executionErr, infrastructureErr).Error()
	}
	var product *engine.ProductFailure
	if errors.As(executionErr, &product) {
		return ProductFailed, executionErr.Error()
	}
	if row.TransportFloods > 0 {
		return Unavailable, "transport flood; action interval does not measure transport RPC capacity"
	}
	if row.FloodWaits > 0 {
		for _, op := range strings.Split(row.FloodOps, ",") {
			switch op {
			case "send_text", "edit_text", "send_photo", "send_photo_group", "send_document", "send_voice", "send_audio", "click_button", "delete_messages":
			default:
				return Unavailable, "RPC flood outside measured actions: " + row.FloodOps
			}
		}
		return RateLimited, "Telegram action flood: " + row.FloodOps
	}
	if executionErr != nil {
		return Unavailable, executionErr.Error()
	}
	if row.CompletedRuns != row.Runs {
		return Unavailable, "requested runs not completed"
	}
	return Passed, "all requested runs completed"
}

func traceCommand(w io.Writer, phase string, cmd protocol.Command, err error) {
	if w == nil {
		return
	}
	now := time.Now().UTC().Format(time.RFC3339)
	if err != nil {
		fmt.Fprintf(w, "COMMAND_%s at=%s id=%q action=%q chat=%q error=%q\n",
			strings.ToUpper(phase), now, cmd.ID, cmd.Action, cmd.Chat, err.Error())
		return
	}
	fmt.Fprintf(w, "COMMAND_%s at=%s id=%q action=%q chat=%q\n",
		strings.ToUpper(phase), now, cmd.ID, cmd.Action, cmd.Chat)
}

func appendFloodEvents(logPath string, events []mtproto.FloodEvent) error {
	if len(events) == 0 {
		return nil
	}
	file, err := os.OpenFile(logPath, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	var writeErr error
	for _, event := range events {
		if _, err := fmt.Fprintf(file, "FLOOD_EVENT at=%s kind=%s op=%s delay_ms=%d error=%q\n", event.At.Format(time.RFC3339), event.Kind, event.Operation, event.Delay.Milliseconds(), event.Error); err != nil {
			writeErr = err
			break
		}
	}
	return errors.Join(writeErr, file.Close())
}

func summarizeFloodOps(events []mtproto.FloodEvent, statsBefore mtproto.Stats, statsAfter mtproto.Stats) string {
	expected := (statsAfter.FloodWaits - statsBefore.FloodWaits) + (statsAfter.TransportFloods - statsBefore.TransportFloods)
	if expected <= 0 || len(events) == 0 {
		return ""
	}
	start := len(events) - expected
	if start < 0 {
		start = 0
	}
	seen := map[string]struct{}{}
	ordered := make([]string, 0, expected)
	for _, event := range events[start:] {
		if _, ok := seen[event.Operation]; ok {
			continue
		}
		seen[event.Operation] = struct{}{}
		ordered = append(ordered, event.Operation)
	}
	return strings.Join(ordered, ",")
}

func saveTranscriptSet(dir string, prefix string, tr *transcript.Transcript) error {
	if err := tr.SaveJSON(filepath.Join(dir, prefix+".json")); err != nil {
		return fmt.Errorf("save JSON transcript: %w", err)
	}
	if err := tr.SaveText(filepath.Join(dir, prefix+".txt")); err != nil {
		return fmt.Errorf("save text transcript: %w", err)
	}
	return nil
}

func writeSummary(path string, rows []SummaryRow) error {
	var buffer strings.Builder
	w := &buffer
	fmt.Fprintln(w, "probe\taction_spacing_ms\tscenario\truns\texit_code\telapsed_ms\tflood_waits\ttransport_floods\tflood_ops\tpassed\tphase\toutcome\tcompleted_runs\treason")
	for _, row := range rows {
		fmt.Fprintf(w, "%d\t%d\t%s\t%d\t%d\t%d\t%d\t%d\t%s\t%t\t%s\t%s\t%d\t%q\n",
			row.ProbeIndex,
			row.ActionSpacingMS,
			row.Scenario,
			row.Runs,
			row.ExitCode,
			row.ElapsedMS,
			row.FloodWaits,
			row.TransportFloods,
			row.FloodOps,
			row.Passed, row.Phase, row.Outcome, row.CompletedRuns, row.Reason,
		)
	}
	return writeAtomic(path, []byte(buffer.String()))
}

func renderSummaryTable(out io.Writer, rows []SummaryRow) {
	tw := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "probe\taction_spacing_ms\tscenario\truns\texit_code\telapsed_ms\tflood_waits\ttransport_floods\tflood_ops\tpassed\tphase\toutcome\tcompleted_runs\treason")
	for _, row := range rows {
		fmt.Fprintf(tw, "%d\t%d\t%s\t%d\t%d\t%d\t%d\t%d\t%s\t%t\t%s\t%s\t%d\t%q\n",
			row.ProbeIndex,
			row.ActionSpacingMS,
			row.Scenario,
			row.Runs,
			row.ExitCode,
			row.ElapsedMS,
			row.FloodWaits,
			row.TransportFloods,
			row.FloodOps,
			row.Passed, row.Phase, row.Outcome, row.CompletedRuns, row.Reason,
		)
	}
	_ = tw.Flush()
}

func writeRecommendation(path string, rec Recommendation, opts Options) error {
	var buffer strings.Builder
	w := &buffer
	fmt.Fprintf(w, "found=%t\nconfirmed=%t\nconfirmation_probe=%d\nerror=%s\n", rec.Found, rec.Confirmed, rec.ProbeIndex, rec.Error)
	fmt.Fprintf(w, "limit_reached=%t\n", rec.LimitReached)
	fmt.Fprintf(w, "action_spacing_ms=%d\n", rec.ActionSpacingMS)
	fmt.Fprintf(w, "sync_interval_ms=%d\n", rec.SyncIntervalMS)
	fmt.Fprintf(w, "rpc_spacing_ms=%d\n", rec.RPCSpacingMS)
	fmt.Fprintf(w, "pinned_cache_ttl_ms=%d\n", rec.PinnedTTLMS)
	fmt.Fprintf(w, "runs=%d\n", opts.Runs)
	fmt.Fprintf(w, "search_min_action_ms=%d\n", opts.MinActionSpacing.Milliseconds())
	fmt.Fprintf(w, "search_max_action_ms=%d\n", opts.MaxActionSpacing.Milliseconds())
	fmt.Fprintf(w, "resolution_ms=%d\n", opts.Resolution.Milliseconds())
	return writeAtomic(path, []byte(buffer.String()))
}

func renderRecommendation(out io.Writer, rec Recommendation, opts Options) {
	if !rec.Found {
		fmt.Fprintf(out, "no safe action_spacing found in [%dms, %dms] at %dms resolution\n",
			opts.MinActionSpacing.Milliseconds(),
			opts.MaxActionSpacing.Milliseconds(),
			opts.Resolution.Milliseconds(),
		)
		return
	}
	fmt.Fprintf(out, "recommended action_spacing=%dms (sync=%dms rpc=%dms pinned=%dms)\n",
		rec.ActionSpacingMS,
		rec.SyncIntervalMS,
		rec.RPCSpacingMS,
		rec.PinnedTTLMS,
	)
	if !rec.LimitReached {
		fmt.Fprintf(out, "lowest tested action_spacing already passed; limit was not reached inside [%dms, %dms]\n",
			opts.MinActionSpacing.Milliseconds(),
			opts.MaxActionSpacing.Milliseconds(),
		)
	}
}

func writeAtomic(path string, body []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	file, err := os.CreateTemp(filepath.Dir(path), ".sweep-*")
	if err != nil {
		return err
	}
	defer os.Remove(file.Name())
	_, writeErr := file.Write(body)
	if err := errors.Join(writeErr, file.Close()); err != nil {
		return err
	}
	return os.Rename(file.Name(), path)
}
