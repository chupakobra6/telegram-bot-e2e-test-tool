package ratesweep

import (
	"bufio"
	"context"
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

type ProbeResult struct {
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
	Found           bool
	LimitReached    bool
	ActionSpacingMS int64
	SyncIntervalMS  int64
	RPCSpacingMS    int64
	PinnedTTLMS     int64
}

func Run(ctx context.Context, baseCfg config.Config, stdout io.Writer, opts Options) error {
	if stdout == nil {
		stdout = io.Discard
	}
	normalized, err := normalizeOptions(opts)
	if err != nil {
		return err
	}
	if err := validateScenarioInputs(normalized.ScenarioPaths, normalized.TargetChat); err != nil {
		return err
	}
	if err := validateScenarioInputs(normalized.PreparePaths, normalized.TargetChat); err != nil {
		return err
	}

	stamp := normalized.Now().UTC().Format("20060102T150405Z")
	outDir := filepath.Join(normalized.ArtifactRoot, stamp)
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return fmt.Errorf("create rate sweep directory: %w", err)
	}
	summaryPath := filepath.Join(outDir, "summary.tsv")
	recommendationPath := filepath.Join(outDir, "recommendation.txt")

	probeRows := make([]SummaryRow, 0)
	probeResults := make([]ProbeResult, 0)
	recommendation := Recommendation{
		Found:           false,
		LimitReached:    true,
		SyncIntervalMS:  normalized.ProbeSyncInterval.Milliseconds(),
		RPCSpacingMS:    normalized.ProbeRPCSpacing.Milliseconds(),
		PinnedTTLMS:     normalized.ProbePinnedTTL.Milliseconds(),
		ActionSpacingMS: 0,
	}

	client := mtproto.New(baseCfg)
	if err := client.RunAuthorized(ctx, func(ctx context.Context, session *mtproto.Session) error {
		session.SetFloodWaitRetry(false)
		candidates := buildCandidates(normalized.MinActionSpacing, normalized.MaxActionSpacing, normalized.Resolution)
		lo := 0
		hi := len(candidates) - 1
		bestIdx := -1
		probeCache := map[int]ProbeResult{}
		probeOrder := 0

		for lo <= hi {
			mid := lo + (hi-lo)/2
			candidate := candidates[mid]
			result, ok := probeCache[mid]
			if !ok {
				probeOrder++
				fmt.Fprintf(stdout, "==> probe=%d action=%dms sync=%dms rpc=%dms pinned=%dms\n",
					probeOrder,
					candidate.Milliseconds(),
					normalized.ProbeSyncInterval.Milliseconds(),
					normalized.ProbeRPCSpacing.Milliseconds(),
					normalized.ProbePinnedTTL.Milliseconds(),
				)
				var err error
				result, err = runProbe(ctx, session, baseCfg, normalized, outDir, candidate, probeOrder)
				if err != nil {
					return err
				}
				probeCache[mid] = result
				probeResults = append(probeResults, result)
				probeRows = append(probeRows, result.ScenarioRows...)
				fmt.Fprintf(stdout, "   result=%t elapsed=%dms flood_waits=%d transport_floods=%d reason=%s\n",
					result.Passed,
					result.TotalElapsedMS,
					result.FloodWaits,
					result.TransportFloods,
					result.Reason,
				)
			}

			if result.Passed {
				bestIdx = mid
				hi = mid - 1
				continue
			}
			lo = mid + 1
		}

		if bestIdx >= 0 {
			recommendation.Found = true
			recommendation.ActionSpacingMS = candidates[bestIdx].Milliseconds()
			recommendation.LimitReached = bestIdx != 0
		}
		return nil
	}); err != nil {
		return err
	}

	if err := writeSummary(summaryPath, probeRows); err != nil {
		return err
	}
	if err := writeRecommendation(recommendationPath, recommendation, normalized); err != nil {
		return err
	}

	fmt.Fprintln(stdout)
	fmt.Fprintf(stdout, "rate sweep summary: %s\n", summaryPath)
	renderSummaryTable(stdout, probeRows)
	fmt.Fprintln(stdout)
	fmt.Fprintf(stdout, "recommendation: %s\n", recommendationPath)
	renderRecommendation(stdout, recommendation, normalized)

	return nil
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

func runProbe(
	ctx context.Context,
	session *mtproto.Session,
	baseCfg config.Config,
	opts Options,
	outDir string,
	actionSpacing time.Duration,
	probeIndex int,
) (ProbeResult, error) {
	result := ProbeResult{
		ProbeIndex:      probeIndex,
		ActionSpacingMS: actionSpacing.Milliseconds(),
		Passed:          true,
		Reason:          "ok",
	}

	transcriptDir := filepath.Join(outDir, "transcripts", fmt.Sprintf("%02d-%dms", probeIndex, actionSpacing.Milliseconds()))
	readOpts := scenario.ReadOptions{TargetChat: opts.TargetChat}
	if len(opts.PreparePaths) > 0 {
		if err := session.Cooldown(ctx, DefaultProbeCooldown); err != nil {
			return ProbeResult{}, err
		}
		session.ConfigurePacing(baseCfg.ActionSpacing, baseCfg.RPCSpacing, baseCfg.PinnedCacheTTL)
		for _, preparePath := range opts.PreparePaths {
			row, err := runScenarioBatch(ctx, session, baseCfg, actionSpacing, probeIndex, transcriptDir, preparePath, readOpts, baseCfg.SyncInterval, 1)
			if err != nil {
				return ProbeResult{}, err
			}
			if !row.Passed {
				result.Passed = false
				result.Reason = scenarioFailureReason("prepare", row)
				return result, nil
			}
		}
	}
	if err := session.Cooldown(ctx, DefaultProbeCooldown); err != nil {
		return ProbeResult{}, err
	}
	session.ConfigurePacing(actionSpacing, opts.ProbeRPCSpacing, opts.ProbePinnedTTL)

	statsBeforeProbe := session.Stats()
	startProbe := time.Now()

	for _, scenarioPath := range opts.ScenarioPaths {
		row, err := runScenarioBatch(ctx, session, baseCfg, actionSpacing, probeIndex, transcriptDir, scenarioPath, readOpts, opts.ProbeSyncInterval, opts.Runs)
		if err != nil {
			return ProbeResult{}, err
		}
		result.ScenarioRows = append(result.ScenarioRows, row)
		if !row.Passed {
			result.Passed = false
			result.Reason = scenarioFailureReason("scenario", row)
			break
		}
	}

	statsAfterProbe := session.Stats()
	result.TotalElapsedMS = time.Since(startProbe).Milliseconds()
	result.FloodWaits = statsAfterProbe.FloodWaits - statsBeforeProbe.FloodWaits
	result.TransportFloods = statsAfterProbe.TransportFloods - statsBeforeProbe.TransportFloods
	if result.Passed && (result.FloodWaits > 0 || result.TransportFloods > 0) {
		result.Passed = false
		if result.TransportFloods > 0 {
			result.Reason = "transport flood"
		} else {
			result.Reason = "flood wait"
		}
	}
	return result, nil
}

func runScenarioBatch(
	ctx context.Context,
	session *mtproto.Session,
	baseCfg config.Config,
	actionSpacing time.Duration,
	probeIndex int,
	transcriptDir string,
	scenarioPath string,
	readOpts scenario.ReadOptions,
	syncInterval time.Duration,
	runs int,
) (SummaryRow, error) {
	baseName := strings.TrimSuffix(filepath.Base(scenarioPath), filepath.Ext(scenarioPath))
	statsBeforeScenario := session.Stats()
	start := time.Now()
	exitCode := 0

	for runIndex := 0; runIndex < runs; runIndex++ {
		prefix := fmt.Sprintf("%s__run-%02d", baseName, runIndex+1)
		logPath := filepath.Join(transcriptDir, prefix+".log")
		if err := os.MkdirAll(filepath.Dir(logPath), 0o755); err != nil {
			return SummaryRow{}, fmt.Errorf("create log directory: %w", err)
		}
		logFile, err := os.Create(logPath)
		if err != nil {
			return SummaryRow{}, fmt.Errorf("create log file: %w", err)
		}

		eventsBefore := len(session.FloodEvents())
		tr := transcript.New()
		runner := engine.New(session, tr, logFile, baseCfg.HistoryWindow, syncInterval)
		err = scenario.ReadWithOptions(scenarioPath, readOpts, func(cmd protocol.Command) error {
			traceCommand(logFile, "start", cmd, nil)
			err := runner.Execute(ctx, cmd)
			if err != nil {
				traceCommand(logFile, "error", cmd, err)
				return err
			}
			traceCommand(logFile, "done", cmd, nil)
			return nil
		})
		closeErr := logFile.Close()
		if closeErr != nil && err == nil {
			err = closeErr
		}
		eventsAfter := session.FloodEvents()
		appendFloodEvents(logPath, eventsAfter[eventsBefore:])
		if saveErr := saveTranscriptSet(transcriptDir, prefix, tr); saveErr != nil {
			return SummaryRow{}, saveErr
		}
		if err != nil {
			exitCode = 1
			break
		}
	}

	statsAfterScenario := session.Stats()
	row := SummaryRow{
		ProbeIndex:      probeIndex,
		ActionSpacingMS: actionSpacing.Milliseconds(),
		Scenario:        scenarioPath,
		Runs:            runs,
		ExitCode:        exitCode,
		ElapsedMS:       time.Since(start).Milliseconds(),
		FloodWaits:      statsAfterScenario.FloodWaits - statsBeforeScenario.FloodWaits,
		TransportFloods: statsAfterScenario.TransportFloods - statsBeforeScenario.TransportFloods,
	}
	row.FloodOps = summarizeFloodOps(session.FloodEvents(), statsBeforeScenario, statsAfterScenario)
	row.Passed = row.ExitCode == 0 && row.FloodWaits == 0 && row.TransportFloods == 0
	return row, nil
}

func scenarioFailureReason(kind string, row SummaryRow) string {
	if row.ExitCode != 0 {
		return kind + " runtime error"
	}
	if row.TransportFloods > 0 {
		return kind + " transport flood"
	}
	if row.FloodWaits > 0 {
		return kind + " flood wait"
	}
	return kind + " failed"
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

func appendFloodEvents(logPath string, events []mtproto.FloodEvent) {
	if len(events) == 0 {
		return
	}
	file, err := os.OpenFile(logPath, os.O_APPEND|os.O_WRONLY|os.O_CREATE, 0o644)
	if err != nil {
		return
	}
	defer file.Close()
	for _, event := range events {
		fmt.Fprintf(file, "FLOOD_EVENT at=%s kind=%s op=%s delay_ms=%d error=%q\n",
			event.At.Format(time.RFC3339),
			event.Kind,
			event.Operation,
			event.Delay.Milliseconds(),
			event.Error,
		)
	}
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
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create summary directory: %w", err)
	}
	file, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("create summary file: %w", err)
	}
	defer file.Close()

	w := bufio.NewWriter(file)
	fmt.Fprintln(w, "probe\taction_spacing_ms\tscenario\truns\texit_code\telapsed_ms\tflood_waits\ttransport_floods\tflood_ops\tpassed")
	for _, row := range rows {
		fmt.Fprintf(w, "%d\t%d\t%s\t%d\t%d\t%d\t%d\t%d\t%s\t%t\n",
			row.ProbeIndex,
			row.ActionSpacingMS,
			row.Scenario,
			row.Runs,
			row.ExitCode,
			row.ElapsedMS,
			row.FloodWaits,
			row.TransportFloods,
			row.FloodOps,
			row.Passed,
		)
	}
	return w.Flush()
}

func renderSummaryTable(out io.Writer, rows []SummaryRow) {
	tw := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "probe\taction_spacing_ms\tscenario\truns\texit_code\telapsed_ms\tflood_waits\ttransport_floods\tflood_ops\tpassed")
	for _, row := range rows {
		fmt.Fprintf(tw, "%d\t%d\t%s\t%d\t%d\t%d\t%d\t%d\t%s\t%t\n",
			row.ProbeIndex,
			row.ActionSpacingMS,
			row.Scenario,
			row.Runs,
			row.ExitCode,
			row.ElapsedMS,
			row.FloodWaits,
			row.TransportFloods,
			row.FloodOps,
			row.Passed,
		)
	}
	_ = tw.Flush()
}

func writeRecommendation(path string, rec Recommendation, opts Options) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create recommendation directory: %w", err)
	}
	file, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("create recommendation file: %w", err)
	}
	defer file.Close()

	w := bufio.NewWriter(file)
	fmt.Fprintf(w, "found=%t\n", rec.Found)
	fmt.Fprintf(w, "limit_reached=%t\n", rec.LimitReached)
	fmt.Fprintf(w, "action_spacing_ms=%d\n", rec.ActionSpacingMS)
	fmt.Fprintf(w, "sync_interval_ms=%d\n", rec.SyncIntervalMS)
	fmt.Fprintf(w, "rpc_spacing_ms=%d\n", rec.RPCSpacingMS)
	fmt.Fprintf(w, "pinned_cache_ttl_ms=%d\n", rec.PinnedTTLMS)
	fmt.Fprintf(w, "runs=%d\n", opts.Runs)
	fmt.Fprintf(w, "search_min_action_ms=%d\n", opts.MinActionSpacing.Milliseconds())
	fmt.Fprintf(w, "search_max_action_ms=%d\n", opts.MaxActionSpacing.Milliseconds())
	fmt.Fprintf(w, "resolution_ms=%d\n", opts.Resolution.Milliseconds())
	return w.Flush()
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
