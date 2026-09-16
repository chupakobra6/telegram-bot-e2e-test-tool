package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/igor/telegram-bot-e2e-test-tool/internal/config"
	"github.com/igor/telegram-bot-e2e-test-tool/internal/fixturegen"
	"github.com/igor/telegram-bot-e2e-test-tool/internal/mtproto"
	"github.com/igor/telegram-bot-e2e-test-tool/internal/textcase"
)

const controlRequestTimeout = 3 * time.Minute

type builtInSuite struct {
	allScenarioRelPaths   []string
	coreScenarioRelPaths  []string
	timedSetupRelPaths    []string
	timedObserveRelPaths  []string
	timedCloseRelPaths    []string
	timedCleanupRelPaths  []string
	paginationRelPaths    []string
	rapidOrderingRelPaths []string
	requiredFixtureNames  []string
}

var defaultSuite = builtInSuite{
	allScenarioRelPaths: []string{
		"examples/suite/01-start-and-stale-dashboard.jsonl",
		"examples/suite/02-dashboard-navigation-and-settings.jsonl",
		"examples/suite/03-text-fast-path-complete.jsonl",
		"examples/suite/04-name-only-edit-name.jsonl",
		"examples/suite/05-edit-date-valid-invalid.jsonl",
		"examples/suite/06-unsupported-document.jsonl",
		"examples/suite/07-photo-unsupported.jsonl",
		"examples/suite/08-product-close-and-stats.jsonl",
		"examples/suite/09-voice-flow.jsonl",
		"examples/suite/10-audio-flow.jsonl",
		"examples/suite/11-timed-digest-setup.jsonl.tmpl",
		"examples/suite/12-timed-digest-observe.jsonl",
		"examples/suite/13-timed-digest-close-product.jsonl.tmpl",
		"examples/suite/14-timed-digest-cleanup-observe.jsonl",
		"examples/suite/15-dashboard-pagination.jsonl.tmpl",
		"examples/suite/16-rapid-interaction-ordering.jsonl",
		"examples/suite/21-draft-incomplete-and-delete.jsonl",
		"examples/suite/22-product-discarded-and-deleted.jsonl",
	},
	coreScenarioRelPaths: []string{
		"examples/suite/01-start-and-stale-dashboard.jsonl",
		"examples/suite/02-dashboard-navigation-and-settings.jsonl",
		"examples/suite/03-text-fast-path-complete.jsonl",
		"examples/suite/04-name-only-edit-name.jsonl",
		"examples/suite/05-edit-date-valid-invalid.jsonl",
		"examples/suite/06-unsupported-document.jsonl",
		"examples/suite/07-photo-unsupported.jsonl",
		"examples/suite/08-product-close-and-stats.jsonl",
		"examples/suite/09-voice-flow.jsonl",
		"examples/suite/10-audio-flow.jsonl",
		"examples/suite/21-draft-incomplete-and-delete.jsonl",
		"examples/suite/22-product-discarded-and-deleted.jsonl",
	},
	timedSetupRelPaths: []string{
		"examples/helpers/00-home-ready.jsonl",
		"examples/suite/11-timed-digest-setup.jsonl.tmpl",
	},
	timedObserveRelPaths: []string{
		"examples/suite/12-timed-digest-observe.jsonl",
	},
	timedCloseRelPaths: []string{
		"examples/suite/13-timed-digest-close-product.jsonl.tmpl",
	},
	timedCleanupRelPaths: []string{
		"examples/suite/14-timed-digest-cleanup-observe.jsonl",
	},
	paginationRelPaths: []string{
		"examples/helpers/00-home-ready.jsonl",
		"examples/suite/15-dashboard-pagination.jsonl.tmpl",
	},
	rapidOrderingRelPaths: []string{
		"examples/helpers/00-home-ready.jsonl",
		"examples/suite/16-rapid-interaction-ordering.jsonl",
	},
	requiredFixtureNames: []string{
		fixturegen.PhotoFixtureName,
		fixturegen.DocumentFixtureName,
		fixturegen.VoiceFixtureName,
		fixturegen.AudioFixtureName,
	},
}

type authorizedClient interface {
	RunAuthorized(context.Context, func(context.Context, *mtproto.Session) error) error
}

func runScenarioCommand(cfg config.Config, client authorizedClient, scenarioPaths []string) error {
	if len(scenarioPaths) < 1 {
		return fmt.Errorf("run-scenario requires at least one JSONL path")
	}
	return runRecorded(cfg, scenarioPaths, func(result *runResult) error {
		sources, err := pathScenarioSources(scenarioPaths, strings.TrimSpace(os.Getenv("CHAT")), nil)
		if err != nil {
			return atStage("read", err)
		}
		return runAuthorizedScenarioSources(client, result, sources)
	})
}

func runBlockCommand(cfg config.Config, client interface {
	RunAuthorized(context.Context, func(context.Context, *mtproto.Session) error) error
}, args []string) error {
	fs := flag.NewFlagSet("run-block", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	noReset := fs.Bool("no-reset", false, "skip /control/e2e/reset before the block")
	clearTime := fs.Bool("clear-time", false, "clear dev time after the block exits")
	controlURL := fs.String("control-url", defaultControlURL(), "dev control base URL")
	runPrefix := fs.String("run-prefix", defaultRunPrefix(), "prefix used when rendering .jsonl.tmpl scenarios")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if len(fs.Args()) < 1 {
		return fmt.Errorf("run-block requires at least one scenario path")
	}
	return runRecorded(cfg, fs.Args(), func(result *runResult) (runErr error) {
		targetChat := strings.TrimSpace(os.Getenv("CHAT"))
		if targetChat == "" {
			return atStage("prepare", fmt.Errorf("CHAT is required"))
		}
		sources, err := pathScenarioSources(fs.Args(), targetChat, runPrefix)
		if err != nil {
			return atStage("read", err)
		}
		if *clearTime {
			defer func() {
				runErr = errors.Join(runErr, atStage("cleanup", postControl(*controlURL, "/control/time/clear")))
			}()
		}
		if !*noReset {
			if err := postControl(*controlURL, "/control/e2e/reset"); err != nil {
				return atStage("prepare", err)
			}
		}
		fmt.Fprintf(os.Stderr, "RUN_PREFIX=%s\n", *runPrefix)
		return runAuthorizedScenarioSources(client, result, sources)
	})
}

func runTextMatrixCommand(cfg config.Config, client interface {
	RunAuthorized(context.Context, func(context.Context, *mtproto.Session) error) error
}, args []string) error {
	fs := flag.NewFlagSet("run-text-matrix", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	casesFile := fs.String("cases", strings.TrimSpace(os.Getenv("CASES")), "path to the text cases file")
	cancelButton := fs.String("cancel-button", defaultString(strings.TrimSpace(os.Getenv("CANCEL_BUTTON_TEXT")), "↩️ Отмена"), "cleanup button text for the transient draft")
	waitTimeoutMS := fs.Int("wait-timeout-ms", defaultInt(os.Getenv("WAIT_TIMEOUT_MS"), 12000), "timeout for wait actions")
	if err := fs.Parse(args); err != nil {
		return err
	}
	return runRecorded(cfg, []string{*casesFile}, func(result *runResult) error {
		targetChat := strings.TrimSpace(os.Getenv("CHAT"))
		if targetChat == "" {
			return fmt.Errorf("CHAT is required")
		}
		if strings.TrimSpace(*casesFile) == "" {
			return fmt.Errorf("CASES or --cases is required")
		}
		resolvedCases := resolveInputPath(*casesFile)
		body, err := os.ReadFile(resolvedCases)
		if err != nil {
			return fmt.Errorf("read cases file: %w", err)
		}

		sources := make([]scenarioSource, 0)
		lines := strings.Split(string(body), "\n")
		caseIndex := 0
		for _, line := range lines {
			phrase := strings.TrimSpace(line)
			if phrase == "" || strings.HasPrefix(phrase, "#") {
				continue
			}
			caseIndex++
			fmt.Fprintf(os.Stdout, "==> text case %d: %s\n", caseIndex, phrase)
			commands := textcase.Render(targetChat, phrase, *cancelButton, *waitTimeoutMS)
			path := fmt.Sprintf("%s#%02d", resolvedCases, caseIndex)
			prefix := fmt.Sprintf("case-%02d", caseIndex)
			sources = append(sources, commandScenarioSource(path, prefix, commands))
		}
		if len(sources) == 0 {
			return fmt.Errorf("no runnable cases found in %s", resolvedCases)
		}

		result.entries = nil
		for _, source := range sources {
			result.plan([]string{source.ScenarioPath})
		}
		if err := result.publish(cfg, "running"); err != nil {
			return atStage("save", err)
		}
		if err := runAuthorizedScenarioSources(client, result, sources); err != nil {
			return err
		}
		fmt.Fprintln(os.Stdout, "text matrix completed")
		return nil
	})
}

func runSuiteCommand(cfg config.Config, client authorizedClient, repoRoot string) error {
	paths := defaultSuite.scenarioPaths(repoRoot)
	paths = append(paths, defaultSuite.timedSetupPaths(repoRoot)...)
	paths = append(paths, defaultSuite.timedObservePaths(repoRoot)...)
	paths = append(paths, defaultSuite.timedClosePaths(repoRoot)...)
	paths = append(paths, defaultSuite.timedCleanupPaths(repoRoot)...)
	paths = append(paths, defaultSuite.paginationPaths(repoRoot)...)
	paths = append(paths, defaultSuite.rapidOrderingPaths(repoRoot)...)
	return runRecorded(cfg, paths, func(result *runResult) error {
		targetChat := strings.TrimSpace(os.Getenv("CHAT"))
		if targetChat == "" {
			return atStage("prepare", fmt.Errorf("CHAT is required"))
		}
		if err := prepareSuiteFixtures(repoRoot); err != nil {
			return atStage("prepare", err)
		}
		controlURL := defaultControlURL()
		return atStage("authorize", client.RunAuthorized(context.Background(), func(ctx context.Context, session *mtproto.Session) error {
			if err := postControl(controlURL, "/control/time/clear"); err != nil {
				return atStage("prepare", err)
			}
			if err := postControl(controlURL, "/control/e2e/reset"); err != nil {
				return atStage("prepare", err)
			}
			if err := runSuitePhase(ctx, session, result, "core", defaultSuite.scenarioPaths(repoRoot), targetChat, nil); err != nil {
				return err
			}
			if err := runTimedDigestSuite(ctx, session, repoRoot, targetChat, controlURL, result); err != nil {
				return err
			}
			if err := postControl(controlURL, "/control/e2e/reset"); err != nil {
				return atStage("prepare", err)
			}
			prefix := defaultRunPrefix()
			if err := runSuitePhase(ctx, session, result, "pagination", defaultSuite.paginationPaths(repoRoot), targetChat, &prefix); err != nil {
				return err
			}
			if err := postControl(controlURL, "/control/e2e/reset"); err != nil {
				return atStage("prepare", err)
			}
			return runSuitePhase(ctx, session, result, "ordering", defaultSuite.rapidOrderingPaths(repoRoot), targetChat, nil)
		}))
	})
}

func runAuthorizedScenarioSources(client authorizedClient, result *runResult, sources []scenarioSource) error {
	return atStage("authorize", client.RunAuthorized(context.Background(), func(ctx context.Context, session *mtproto.Session) error {
		return result.execute(ctx, session, sources)
	}))
}

func pathScenarioSources(paths []string, targetChat string, runPrefix *string) ([]scenarioSource, error) {
	sources := make([]scenarioSource, 0, len(paths))
	for _, path := range paths {
		source, err := pathScenarioSource(path, targetChat, runPrefix)
		if err != nil {
			return nil, err
		}
		sources = append(sources, source)
	}
	return sources, nil
}

func (s builtInSuite) scenarioPaths(repoRoot string) []string {
	paths := make([]string, 0, len(s.coreScenarioRelPaths))
	for _, relPath := range s.coreScenarioRelPaths {
		paths = append(paths, builtInRepoPath(repoRoot, relPath))
	}
	return paths
}

func (s builtInSuite) timedSetupPaths(repoRoot string) []string {
	return builtInRepoPaths(repoRoot, s.timedSetupRelPaths)
}

func (s builtInSuite) timedObservePaths(repoRoot string) []string {
	return builtInRepoPaths(repoRoot, s.timedObserveRelPaths)
}

func (s builtInSuite) timedClosePaths(repoRoot string) []string {
	return builtInRepoPaths(repoRoot, s.timedCloseRelPaths)
}

func (s builtInSuite) timedCleanupPaths(repoRoot string) []string {
	return builtInRepoPaths(repoRoot, s.timedCleanupRelPaths)
}

func (s builtInSuite) paginationPaths(repoRoot string) []string {
	return builtInRepoPaths(repoRoot, s.paginationRelPaths)
}

func (s builtInSuite) rapidOrderingPaths(repoRoot string) []string {
	return builtInRepoPaths(repoRoot, s.rapidOrderingRelPaths)
}

func (s builtInSuite) requiredFixturePaths(repoRoot string) []string {
	paths := make([]string, 0, len(s.requiredFixtureNames))
	for _, name := range s.requiredFixtureNames {
		paths = append(paths, builtInRepoPath(repoRoot, filepath.Join("artifacts", "fixtures", name)))
	}
	return paths
}

func defaultControlURL() string {
	if value := strings.TrimSpace(os.Getenv("CONTROL_URL")); value != "" {
		return value
	}
	if value := strings.TrimSpace(os.Getenv("SHELFY_DEV_CONTROL_URL")); value != "" {
		return value
	}
	return "http://127.0.0.1:8081"
}

func defaultRunPrefix() string {
	if value := strings.TrimSpace(os.Getenv("RUN_PREFIX")); value != "" {
		return value
	}
	return fmt.Sprintf("%x", time.Now().UnixNano())[:10]
}

func postControl(baseURL, path string) error {
	return postControlJSON(baseURL, path, nil)
}

func postControlJSON(baseURL, path string, payload any) error {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if baseURL == "" {
		return fmt.Errorf("control URL is required")
	}
	var bodyReader *bytes.Reader
	if payload == nil {
		bodyReader = bytes.NewReader(nil)
	} else {
		body, err := json.Marshal(payload)
		if err != nil {
			return err
		}
		bodyReader = bytes.NewReader(body)
	}
	req, err := http.NewRequest(http.MethodPost, baseURL+path, bodyReader)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	client := &http.Client{Timeout: controlRequestTimeout}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		return fmt.Errorf("control endpoint %s returned %s", path, resp.Status)
	}
	return nil
}

func runTimedDigestSuite(ctx context.Context, session *mtproto.Session, repoRoot, targetChat, controlURL string, result *runResult) (runErr error) {
	prefix := defaultRunPrefix()
	if err := postControl(controlURL, "/control/e2e/reset"); err != nil {
		return atStage("prepare", err)
	}
	if err := runSuitePhase(ctx, session, result, "timed-digest-setup", defaultSuite.timedSetupPaths(repoRoot), targetChat, &prefix); err != nil {
		return err
	}
	digestAt, err := nextDigestTriggerTime()
	if err != nil {
		return atStage("prepare", err)
	}
	// Clear time even if set returned an error: the server may have applied it.
	defer func() {
		runErr = errors.Join(runErr, atStage("cleanup", postControl(controlURL, "/control/time/clear")))
	}()
	if err := postControlJSON(controlURL, "/control/time/set", map[string]any{"now": digestAt.Format(time.RFC3339)}); err != nil {
		return atStage("prepare", err)
	}
	if err := postControlJSON(controlURL, "/control/jobs/run-due", map[string]any{"include_maintenance": true}); err != nil {
		return atStage("prepare", err)
	}
	if err := runSuitePhase(ctx, session, result, "timed-digest-observe", defaultSuite.timedObservePaths(repoRoot), targetChat, nil); err != nil {
		return err
	}
	if err := runSuitePhase(ctx, session, result, "timed-digest-close", defaultSuite.timedClosePaths(repoRoot), targetChat, &prefix); err != nil {
		return err
	}
	if err := postControl(controlURL, "/control/digests/reconcile"); err != nil {
		return atStage("prepare", err)
	}
	return runSuitePhase(ctx, session, result, "timed-digest-cleanup", defaultSuite.timedCleanupPaths(repoRoot), targetChat, nil)
}

func runSuitePhase(ctx context.Context, session *mtproto.Session, result *runResult, phase string, paths []string, targetChat string, prefix *string) error {
	sources, err := pathScenarioSources(paths, targetChat, prefix)
	if err != nil {
		return atStage("read", err)
	}
	for _, source := range sources {
		fmt.Fprintf(os.Stdout, "==> [%s] %s\n", phase, source.ScenarioPath)
	}
	return result.execute(ctx, session, sources)
}

func nextDigestTriggerTime() (time.Time, error) {
	location, err := time.LoadLocation("Europe/Moscow")
	if err != nil {
		return time.Time{}, err
	}
	now := time.Now().In(location)
	trigger := time.Date(now.Year(), now.Month(), now.Day(), 9, 0, 0, 0, location)
	if !trigger.After(now) {
		trigger = trigger.Add(24 * time.Hour)
	}
	return trigger, nil
}

func builtInRepoPaths(repoRoot string, relPaths []string) []string {
	paths := make([]string, 0, len(relPaths))
	for _, relPath := range relPaths {
		paths = append(paths, builtInRepoPath(repoRoot, relPath))
	}
	return paths
}

func defaultInt(raw string, fallback int) int {
	if strings.TrimSpace(raw) == "" {
		return fallback
	}
	var value int
	if _, err := fmt.Sscanf(raw, "%d", &value); err != nil {
		return fallback
	}
	return value
}

func defaultString(raw, fallback string) string {
	if strings.TrimSpace(raw) == "" {
		return fallback
	}
	return raw
}
