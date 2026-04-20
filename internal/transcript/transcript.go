package transcript

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/igor/telegram-bot-e2e-test-tool/internal/protocol"
)

type Event struct {
	At      time.Time         `json:"at"`
	Kind    string            `json:"kind"`
	Command *protocol.Command `json:"command,omitempty"`
	Output  *protocol.Event   `json:"output,omitempty"`
}

type Transcript struct {
	StartedAt time.Time `json:"started_at"`
	Events    []Event   `json:"events"`
}

func New() *Transcript {
	return &Transcript{StartedAt: time.Now().UTC(), Events: []Event{}}
}

func (t *Transcript) AddCommand(cmd protocol.Command) {
	t.Events = append(t.Events, Event{
		At:      time.Now().UTC(),
		Kind:    "command",
		Command: &cmd,
	})
}

func (t *Transcript) AddEvent(evt protocol.Event) {
	t.Events = append(t.Events, Event{
		At:     time.Now().UTC(),
		Kind:   "event",
		Output: &evt,
	})
}

func (t *Transcript) RenderText() string {
	lines := []string{"Transcript"}
	for _, event := range t.Events {
		switch event.Kind {
		case "command":
			lines = append(lines, fmt.Sprintf(
				"%s [command] id=%s action=%s chat=%s %s",
				event.At.Format(time.RFC3339),
				valueOrDash(event.Command.ID),
				event.Command.Action,
				valueOrDash(event.Command.Chat),
				commandSummary(*event.Command),
			))
		case "event":
			lines = append(lines, fmt.Sprintf(
				"%s [event] type=%s command_id=%s chat=%s ok=%t msg=%s %s",
				event.At.Format(time.RFC3339),
				event.Output.Type,
				valueOrDash(event.Output.CommandID),
				valueOrDash(event.Output.Chat),
				event.Output.OK,
				valueOrDash(event.Output.Message),
				eventSummary(event.Output),
			))
		default:
			lines = append(lines, event.At.Format(time.RFC3339)+" ["+event.Kind+"]")
		}
	}
	return strings.Join(lines, "\n")
}

func (t *Transcript) SaveJSON(path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	body, err := json.MarshalIndent(t, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, body, 0o644)
}

func (t *Transcript) SaveText(path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(t.RenderText()), 0o644)
}

func commandSummary(cmd protocol.Command) string {
	switch cmd.Action {
	case "send_text":
		return "text=" + summarize(cmd.Text)
	case "send_photo", "send_voice", "send_audio":
		return "path=" + summarize(cmd.Path)
	case "click_button":
		return "button_text=" + summarize(cmd.ButtonText)
	case "wait":
		return "timeout_ms=" + strconv.Itoa(cmd.TimeoutMS)
	default:
		return ""
	}
}

func eventSummary(evt *protocol.Event) string {
	parts := make([]string, 0, 4)
	if evt.Error != "" {
		parts = append(parts, "error="+summarize(evt.Error))
	}
	if evt.Diff != nil && evt.Diff.Summary != "" {
		parts = append(parts, "diff="+evt.Diff.Summary)
	}
	if evt.Snapshot != nil {
		parts = append(parts, "messages="+strconv.Itoa(len(evt.Snapshot.Messages)))
		if evt.Snapshot.Pinned != nil {
			parts = append(parts, "pinned="+strconv.Itoa(evt.Snapshot.Pinned.MessageID)+":"+summarize(evt.Snapshot.Pinned.Text))
		}
	}
	return strings.Join(parts, " ")
}

func summarize(v string) string {
	v = strings.TrimSpace(v)
	if v == "" {
		return "-"
	}
	const maxLen = 80
	if len(v) <= maxLen {
		return strconv.Quote(v)
	}
	return strconv.Quote(v[:maxLen-1] + "…")
}

func valueOrDash(v string) string {
	if strings.TrimSpace(v) == "" {
		return "-"
	}
	return v
}
