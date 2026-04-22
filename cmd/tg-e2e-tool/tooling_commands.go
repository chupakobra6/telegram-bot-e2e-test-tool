package main

import (
	"context"
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

type builtInSuite struct {
	scenarioRelPaths     []string
	requiredFixtureNames []string
}

var defaultSuite = builtInSuite{
	scenarioRelPaths: []string{
		"examples/suite/01-start-pin-service.jsonl",
		"examples/suite/02-dashboard-navigation-edit.jsonl",
		"examples/suite/03-text-draft-confirm.jsonl",
		"examples/suite/04-photo-processing-and-draft.jsonl",
		"examples/suite/05-voice-processing.jsonl",
		"examples/suite/06-audio-processing.jsonl",
	},
	requiredFixtureNames: []string{
		fixturegen.PhotoFixtureName,
		fixturegen.VoiceFixtureName,
		fixturegen.AudioFixtureName,
	},
}

func runScenarioCommand(cfg config.Config, client interface {
	RunAuthorized(context.Context, func(context.Context, *mtproto.Session) error) error
}, scenarioPaths []string) error {
	if len(scenarioPaths) < 1 {
		return fmt.Errorf("run-scenario requires at least one JSONL path")
	}
	targetChat := strings.TrimSpace(os.Getenv("CHAT"))
	sources, err := pathScenarioSources(scenarioPaths, targetChat, nil)
	if err != nil {
		return err
	}
	return runAuthorizedScenarioSources(cfg, client, sources)
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
	targetChat := strings.TrimSpace(os.Getenv("CHAT"))
	if targetChat == "" {
		return fmt.Errorf("CHAT is required")
	}
	if len(fs.Args()) < 1 {
		return fmt.Errorf("run-block requires at least one scenario path")
	}

	if !*noReset {
		if err := postControl(*controlURL, "/control/e2e/reset"); err != nil {
			return err
		}
	}
	if *clearTime {
		defer func() {
			_ = postControl(*controlURL, "/control/time/clear")
		}()
	}

	sources, err := pathScenarioSources(fs.Args(), targetChat, runPrefix)
	if err != nil {
		return err
	}

	fmt.Fprintf(os.Stderr, "RUN_PREFIX=%s\n", *runPrefix)
	return runAuthorizedScenarioSources(cfg, client, sources)
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

	if err := runAuthorizedScenarioSources(cfg, client, sources); err != nil {
		return err
	}
	fmt.Fprintln(os.Stdout, "text matrix completed")
	return nil
}

func runSuiteCommand(cfg config.Config, client interface {
	RunAuthorized(context.Context, func(context.Context, *mtproto.Session) error) error
}, repoRoot string) error {
	targetChat := strings.TrimSpace(os.Getenv("CHAT"))
	if targetChat == "" {
		return fmt.Errorf("CHAT is required")
	}

	scenarioPaths := defaultSuite.scenarioPaths(repoRoot)
	requiredFixtures := defaultSuite.requiredFixturePaths(repoRoot)

	for _, scenarioPath := range scenarioPaths {
		fmt.Fprintf(os.Stdout, "==> running %s\n", scenarioPath)
	}
	sources, err := pathScenarioSources(scenarioPaths, targetChat, nil)
	if err != nil {
		return err
	}
	for _, fixture := range requiredFixtures {
		if !fileExists(fixture) {
			return fmt.Errorf("required fixture missing: %s (run `tg-e2e-tool fixtures` first)", fixture)
		}
	}

	if err := runAuthorizedScenarioSources(cfg, client, sources); err != nil {
		return err
	}
	fmt.Fprintln(os.Stdout, "suite completed")
	return nil
}

func runAuthorizedScenarioSources(cfg config.Config, client interface {
	RunAuthorized(context.Context, func(context.Context, *mtproto.Session) error) error
}, sources []scenarioSource) error {
	return withRuntimeLock(cfg, func() error {
		return client.RunAuthorized(context.Background(), func(ctx context.Context, session *mtproto.Session) error {
			return runScenarioSources(ctx, cfg, session, sources, os.Stdout, os.Stderr)
		})
	})
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
	paths := make([]string, 0, len(s.scenarioRelPaths))
	for _, relPath := range s.scenarioRelPaths {
		paths = append(paths, builtInRepoPath(repoRoot, relPath))
	}
	return paths
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
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if baseURL == "" {
		return fmt.Errorf("control URL is required")
	}
	req, err := http.NewRequest(http.MethodPost, baseURL+path, nil)
	if err != nil {
		return err
	}
	client := &http.Client{Timeout: 15 * time.Second}
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
