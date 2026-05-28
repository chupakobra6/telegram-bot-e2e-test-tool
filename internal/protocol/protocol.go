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
	ID              string   `json:"id,omitempty"`
	Action          string   `json:"action"`
	Chat            string   `json:"chat,omitempty"`
	Topic           int      `json:"topic,omitempty"`
	MessageThreadID int      `json:"message_thread_id,omitempty"`
	TimeoutMS       int      `json:"timeout_ms,omitempty"`
	MessageOffset   int      `json:"message_offset,omitempty"`
	Text            string   `json:"text,omitempty"`
	Path            string   `json:"path,omitempty"`
	Paths           []string `json:"paths,omitempty"`
	Caption         string   `json:"caption,omitempty"`
	ButtonText      string   `json:"button_text,omitempty"`
	MessageText     string   `json:"message_text,omitempty"`
}

type Event struct {
	Type            string           `json:"type"`
	CommandID       string           `json:"command_id,omitempty"`
	Action          string           `json:"action,omitempty"`
	Chat            string           `json:"chat,omitempty"`
	MessageThreadID int              `json:"message_thread_id,omitempty"`
	OK              bool             `json:"ok,omitempty"`
	Message         string           `json:"message,omitempty"`
	Error           string           `json:"error,omitempty"`
	Snapshot        *state.ChatState `json:"snapshot,omitempty"`
	Diff            *state.ChatDiff  `json:"diff,omitempty"`
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
	if err := c.validateThread(); err != nil {
		return err
	}
	switch c.Action {
	case "select_chat":
		if strings.TrimSpace(c.Chat) == "" {
			return fmt.Errorf("select_chat requires chat")
		}
	case "send_text":
		if strings.TrimSpace(c.Text) == "" {
			return fmt.Errorf("send_text requires text")
		}
	case "send_photo", "send_document":
		if strings.TrimSpace(c.Path) == "" {
			return fmt.Errorf("%s requires path", c.Action)
		}
	case "send_photo_group":
		if len(c.Paths) < 2 {
			return fmt.Errorf("send_photo_group requires at least two paths")
		}
		if len(c.Paths) > 10 {
			return fmt.Errorf("send_photo_group supports at most ten paths")
		}
		for i, path := range c.Paths {
			if strings.TrimSpace(path) == "" {
				return fmt.Errorf("send_photo_group path %d is empty", i+1)
			}
		}
	case "send_voice", "send_audio":
		if strings.TrimSpace(c.Path) == "" {
			return fmt.Errorf("%s requires path", c.Action)
		}
	case "click_button":
		if strings.TrimSpace(c.ButtonText) == "" {
			return fmt.Errorf("click_button requires button_text")
		}
		if c.MessageOffset < 0 {
			return fmt.Errorf("click_button requires message_offset >= 0")
		}
	case "wait", "dump_state":
	default:
		return fmt.Errorf("unsupported action %q", c.Action)
	}
	return nil
}

func (c Command) ThreadID() int {
	if c.MessageThreadID != 0 {
		return c.MessageThreadID
	}
	return c.Topic
}

func (c Command) HasThreadID() bool {
	return c.Topic != 0 || c.MessageThreadID != 0
}

func (c Command) validateThread() error {
	if c.Topic < 0 {
		return fmt.Errorf("topic requires topic >= 0")
	}
	if c.MessageThreadID < 0 {
		return fmt.Errorf("message_thread_id requires message_thread_id >= 0")
	}
	if c.Topic != 0 && c.MessageThreadID != 0 && c.Topic != c.MessageThreadID {
		return fmt.Errorf("topic and message_thread_id must match when both are set")
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
