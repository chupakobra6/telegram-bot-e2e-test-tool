package protocol

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/igor/telegram-bot-e2e-test-tool/internal/state"
)

type Command struct {
	ID         string `json:"id,omitempty"`
	Action     string `json:"action"`
	Chat       string `json:"chat,omitempty"`
	TimeoutMS  int    `json:"timeout_ms,omitempty"`
	Text       string `json:"text,omitempty"`
	Path       string `json:"path,omitempty"`
	Caption    string `json:"caption,omitempty"`
	ButtonText string `json:"button_text,omitempty"`
}

type Event struct {
	Type      string           `json:"type"`
	CommandID string           `json:"command_id,omitempty"`
	Action    string           `json:"action,omitempty"`
	Chat      string           `json:"chat,omitempty"`
	OK        bool             `json:"ok,omitempty"`
	Message   string           `json:"message,omitempty"`
	Error     string           `json:"error,omitempty"`
	Snapshot  *state.ChatState `json:"snapshot,omitempty"`
	Diff      *state.ChatDiff  `json:"diff,omitempty"`
}

func ParseCommandLine(line []byte) (Command, error) {
	var cmd Command
	if err := json.Unmarshal(line, &cmd); err != nil {
		return Command{}, fmt.Errorf("parse command: %w", err)
	}
	if err := cmd.Validate(); err != nil {
		return Command{}, err
	}
	return cmd, nil
}

func (c Command) Validate() error {
	if strings.TrimSpace(c.Action) == "" {
		return fmt.Errorf("command action is required")
	}
	switch c.Action {
	case "send_text":
		if strings.TrimSpace(c.Text) == "" {
			return fmt.Errorf("send_text requires text")
		}
	case "send_photo":
		if strings.TrimSpace(c.Path) == "" {
			return fmt.Errorf("send_photo requires path")
		}
	case "send_voice", "send_audio":
		if strings.TrimSpace(c.Path) == "" {
			return fmt.Errorf("%s requires path", c.Action)
		}
	case "click_button":
		if strings.TrimSpace(c.ButtonText) == "" {
			return fmt.Errorf("click_button requires button_text")
		}
	case "wait", "dump_state":
	default:
		return fmt.Errorf("unsupported action %q", c.Action)
	}
	return nil
}

func EncodeEvent(w io.Writer, event Event) error {
	enc := json.NewEncoder(w)
	return enc.Encode(event)
}

func ReadCommands(r io.Reader, fn func(Command) error) error {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 1024), 1024*1024)
	for scanner.Scan() {
		line := bytes.TrimSpace(scanner.Bytes())
		if len(line) == 0 || bytes.HasPrefix(line, []byte("#")) {
			continue
		}
		cmd, err := ParseCommandLine(line)
		if err != nil {
			return err
		}
		if err := fn(cmd); err != nil {
			return err
		}
	}
	return scanner.Err()
}
