package scenario

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/igor/telegram-bot-e2e-test-tool/internal/protocol"
)

const DefaultChatPlaceholder = "@your_bot_username"
const DefaultFixturePlaceholder = "@fixtures/"
const DefaultFixtureDir = "artifacts/fixtures"

type ReadOptions struct {
	TargetChat string
	FixtureDir string
}

func Read(path string, fn func(protocol.Command) error) error {
	return ReadWithOptions(path, ReadOptions{}, fn)
}

func ReadWithOptions(path string, opts ReadOptions, fn func(protocol.Command) error) error {
	file, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("open scenario: %w", err)
	}
	defer file.Close()

	if err := ReadReaderWithOptions(filepath.Dir(path), file, opts, fn); err != nil {
		return fmt.Errorf("read scenario: %w", err)
	}
	return nil
}

func ReadBytesWithOptions(path string, body []byte, opts ReadOptions, fn func(protocol.Command) error) error {
	if err := ReadReaderWithOptions(filepath.Dir(path), bytes.NewReader(body), opts, fn); err != nil {
		return fmt.Errorf("read scenario: %w", err)
	}
	return nil
}

func ReadReaderWithOptions(baseDir string, r io.Reader, opts ReadOptions, fn func(protocol.Command) error) error {
	return protocol.ReadCommands(r, func(cmd protocol.Command) error {
		if cmd.Chat == DefaultChatPlaceholder {
			if opts.TargetChat == "" {
				return fmt.Errorf("scenario uses %s; provide CHAT=@your_bot_username for this run", DefaultChatPlaceholder)
			}
			cmd.Chat = opts.TargetChat
		}
		if cmd.Path != "" {
			switch {
			case strings.HasPrefix(cmd.Path, DefaultFixturePlaceholder):
				fixtureDir := opts.FixtureDir
				if strings.TrimSpace(fixtureDir) == "" {
					fixtureDir = DefaultFixtureDir
				}
				cmd.Path = filepath.Join(fixtureDir, strings.TrimPrefix(cmd.Path, DefaultFixturePlaceholder))
			case !filepath.IsAbs(cmd.Path):
				cmd.Path = filepath.Join(baseDir, cmd.Path)
			}
		}
		return fn(cmd)
	})
}

func UsesChatPlaceholder(path string) (bool, error) {
	body, err := os.ReadFile(path)
	if err != nil {
		return false, fmt.Errorf("read scenario file: %w", err)
	}
	return bytes.Contains(body, []byte(DefaultChatPlaceholder)), nil
}
