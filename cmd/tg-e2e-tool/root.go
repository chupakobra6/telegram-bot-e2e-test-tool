package main

import (
	"os"
	"path/filepath"
	"runtime"

	"github.com/igor/telegram-bot-e2e-test-tool/internal/config"
)

func detectRepoRoot() string {
	if _, file, _, ok := runtime.Caller(0); ok {
		root := filepath.Clean(filepath.Join(filepath.Dir(file), "../.."))
		if fileExists(filepath.Join(root, "go.mod")) {
			return root
		}
	}
	cwd, err := os.Getwd()
	if err != nil {
		return "."
	}
	return cwd
}

func resolveInputPath(path string) string {
	if path == "" || filepath.IsAbs(path) {
		return path
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return path
	}
	return abs
}

func loadToolDotEnv(repoRoot string) error {
	cwd, err := os.Getwd()
	if err == nil {
		if err := config.LoadDotEnv(filepath.Join(cwd, ".env")); err != nil {
			return err
		}
	}
	if repoRoot == "" {
		return nil
	}
	rootEnv := filepath.Join(repoRoot, ".env")
	if err == nil && samePath(cwd, repoRoot) {
		return nil
	}
	return config.LoadDotEnv(rootEnv)
}

func samePath(left, right string) bool {
	if left == "" || right == "" {
		return false
	}
	leftAbs, err := filepath.Abs(left)
	if err != nil {
		return false
	}
	rightAbs, err := filepath.Abs(right)
	if err != nil {
		return false
	}
	return leftAbs == rightAbs
}
