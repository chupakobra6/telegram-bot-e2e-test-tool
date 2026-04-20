package transcript

import (
	"strings"
	"testing"

	"github.com/igor/telegram-bot-e2e-test-tool/internal/protocol"
	"github.com/igor/telegram-bot-e2e-test-tool/internal/state"
)

func TestTranscriptRenderText(t *testing.T) {
	tr := New()
	tr.AddCommand(protocol.Command{ID: "step-1", Action: "send_text", Chat: "@bot", Text: "hello"})
	tr.AddEvent(protocol.Event{
		Type:      "state_update",
		Action:    "send_text",
		CommandID: "step-1",
		Chat:      "@bot",
		Message:   "ok",
		Diff:      &state.ChatDiff{Summary: "added=1"},
		Snapshot: &state.ChatState{
			Messages: []state.VisibleMessage{{ID: 1, Text: "dashboard"}},
			Pinned:   &state.PinnedMessage{MessageID: 1, Text: "dashboard"},
		},
	})
	got := tr.RenderText()
	if got == "" {
		t.Fatalf("expected rendered transcript")
	}
	for _, part := range []string{"step-1", "send_text", "@bot", "added=1", "pinned=1"} {
		if !strings.Contains(got, part) {
			t.Fatalf("RenderText() missing %q in %q", part, got)
		}
	}
}
