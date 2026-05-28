package transcript

import (
	"strings"
	"testing"

	"github.com/igor/telegram-bot-e2e-test-tool/internal/protocol"
	"github.com/igor/telegram-bot-e2e-test-tool/internal/state"
)

func TestRenderTextIncludesMessageThreadID(t *testing.T) {
	tr := New()
	tr.AddCommand(protocol.Command{
		ID:              "select",
		Action:          "select_chat",
		Chat:            "@group",
		MessageThreadID: 123,
	})
	tr.AddEvent(protocol.Event{
		Type:            "state_snapshot",
		CommandID:       "select",
		Action:          "select_chat",
		Chat:            "@group",
		MessageThreadID: 123,
		OK:              true,
		Message:         "chat snapshot captured",
		Snapshot: &state.ChatState{
			Target:          "@group",
			MessageThreadID: 123,
			Messages:        []state.VisibleMessage{},
		},
	})

	rendered := tr.RenderText()
	if !strings.Contains(rendered, "chat=@group thread=123") {
		t.Fatalf("expected command thread in transcript, got:\n%s", rendered)
	}
	if !strings.Contains(rendered, "messages=0 thread=123") {
		t.Fatalf("expected snapshot thread in transcript summary, got:\n%s", rendered)
	}
}
