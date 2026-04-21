package protocol

import (
	"strings"
	"testing"
)

func TestParseCommandLine(t *testing.T) {
	cmd, err := ParseCommandLine([]byte(`{"id":"x","action":"send_text","text":"hello"}`))
	if err != nil {
		t.Fatalf("ParseCommandLine() error = %v", err)
	}
	if cmd.ID != "x" || cmd.Action != "send_text" || cmd.Text != "hello" {
		t.Fatalf("unexpected command: %+v", cmd)
	}
}

func TestParseCommandLineSelectChat(t *testing.T) {
	cmd, err := ParseCommandLine([]byte(`{"id":"select","action":"select_chat","chat":"@bot"}`))
	if err != nil {
		t.Fatalf("ParseCommandLine() error = %v", err)
	}
	if cmd.Action != "select_chat" || cmd.Chat != "@bot" {
		t.Fatalf("unexpected command: %+v", cmd)
	}
}

func TestReadCommandsSkipsCommentsAndBlankLines(t *testing.T) {
	var got []Command
	input := strings.NewReader(`
# comment
{"action":"send_text","text":"hello"}

{"action":"wait","timeout_ms":1000}
`)
	if err := ReadCommands(input, func(cmd Command) error {
		got = append(got, cmd)
		return nil
	}); err != nil {
		t.Fatalf("ReadCommands() error = %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 commands, got %d", len(got))
	}
	if got[0].Action != "send_text" || got[1].Action != "wait" {
		t.Fatalf("unexpected commands: %+v", got)
	}
}

func TestParseCommandLineSendDocument(t *testing.T) {
	cmd, err := ParseCommandLine([]byte(`{"action":"send_document","path":"fixture.txt"}`))
	if err != nil {
		t.Fatalf("ParseCommandLine() error = %v", err)
	}
	if cmd.Action != "send_document" || cmd.Path != "fixture.txt" {
		t.Fatalf("unexpected command: %+v", cmd)
	}
}

func TestParseCommandLineClickButtonNegativeOffsetRejected(t *testing.T) {
	if _, err := ParseCommandLine([]byte(`{"action":"click_button","button_text":"Open","message_offset":-1}`)); err == nil {
		t.Fatal("expected negative message_offset to fail")
	}
}

func TestParseCommandLineSelectChatRequiresChat(t *testing.T) {
	if _, err := ParseCommandLine([]byte(`{"action":"select_chat"}`)); err == nil {
		t.Fatal("expected missing chat to fail")
	}
}
