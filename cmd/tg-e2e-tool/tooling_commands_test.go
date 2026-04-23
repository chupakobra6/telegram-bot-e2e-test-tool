package main

import (
	"path/filepath"
	"reflect"
	"sort"
	"testing"

	"github.com/igor/telegram-bot-e2e-test-tool/internal/fixturegen"
	"github.com/igor/telegram-bot-e2e-test-tool/internal/protocol"
)

func TestDefaultSuiteMatchesBundledScenarioFiles(t *testing.T) {
	repoRoot := detectRepoRoot()

	patterns := []string{
		filepath.Join(repoRoot, "examples", "suite", "*.jsonl"),
		filepath.Join(repoRoot, "examples", "suite", "*.jsonl.tmpl"),
	}
	got := make([]string, 0)
	for _, pattern := range patterns {
		matches, err := filepath.Glob(pattern)
		if err != nil {
			t.Fatalf("Glob(%q) error = %v", pattern, err)
		}
		for _, match := range matches {
			rel, err := filepath.Rel(repoRoot, match)
			if err != nil {
				t.Fatalf("Rel() error = %v", err)
			}
			got = append(got, filepath.ToSlash(rel))
		}
	}
	sort.Strings(got)

	want := append([]string(nil), defaultSuite.allScenarioRelPaths...)
	sort.Strings(want)

	if !reflect.DeepEqual(got, want) {
		t.Fatalf("bundled suite mismatch:\n got: %#v\nwant: %#v", got, want)
	}
}

func TestDefaultSuiteRequiresAllBundledFixtures(t *testing.T) {
	want := []string{
		fixturegen.PhotoFixtureName,
		fixturegen.DocumentFixtureName,
		fixturegen.VoiceFixtureName,
		fixturegen.AudioFixtureName,
	}
	if !reflect.DeepEqual(defaultSuite.requiredFixtureNames, want) {
		t.Fatalf("requiredFixtureNames = %#v, want %#v", defaultSuite.requiredFixtureNames, want)
	}
}

func TestBundledScenariosParse(t *testing.T) {
	repoRoot := detectRepoRoot()
	patterns := []string{
		filepath.Join(repoRoot, "examples", "suite", "*.jsonl"),
		filepath.Join(repoRoot, "examples", "suite", "*.jsonl.tmpl"),
		filepath.Join(repoRoot, "examples", "helpers", "*.jsonl"),
		filepath.Join(repoRoot, "examples", "bench", "*.jsonl"),
		filepath.Join(repoRoot, "examples", "shelfy-smoke.jsonl"),
	}

	files := make([]string, 0)
	for _, pattern := range patterns {
		matches, err := filepath.Glob(pattern)
		if err != nil {
			t.Fatalf("Glob(%q) error = %v", pattern, err)
		}
		files = append(files, matches...)
	}
	sort.Strings(files)

	if len(files) == 0 {
		t.Fatal("expected bundled scenarios")
	}

	for _, file := range files {
		file := file
		t.Run(filepath.Base(file), func(t *testing.T) {
			commands := 0
			runPrefix := "testrun"
			source, err := pathScenarioSource(file, "@example_bot", &runPrefix)
			if err != nil {
				t.Fatalf("pathScenarioSource() error = %v", err)
			}
			err = source.Read(func(_ protocol.Command) error {
				commands++
				return nil
			})
			if err != nil {
				t.Fatalf("ReadWithOptions() error = %v", err)
			}
			if commands == 0 {
				t.Fatal("expected at least one command")
			}
		})
	}
}
