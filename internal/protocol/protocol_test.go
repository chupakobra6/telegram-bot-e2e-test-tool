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

func TestParseCommandLineSendPhotoGroup(t *testing.T) {
	cmd, err := ParseCommandLine([]byte(`{"action":"send_photo_group","paths":["one.png","two.png"],"caption":"общая подпись"}`))
	if err != nil {
		t.Fatalf("ParseCommandLine() error = %v", err)
	}
	if cmd.Action != "send_photo_group" || len(cmd.Paths) != 2 || cmd.Caption != "общая подпись" {
		t.Fatalf("unexpected command: %+v", cmd)
	}
}

func TestParseCommandLineSendPhotoGroupRequiresTwoPaths(t *testing.T) {
	if _, err := ParseCommandLine([]byte(`{"action":"send_photo_group","paths":["one.png"]}`)); err == nil {
		t.Fatal("expected single-photo group to fail")
	}
}

func TestParseCommandLineTopicAlias(t *testing.T) {
	cmd, err := ParseCommandLine([]byte(`{"action":"send_text","topic":123,"text":"hello"}`))
	if err != nil {
		t.Fatalf("ParseCommandLine() error = %v", err)
	}
	if cmd.ThreadID() != 123 || !cmd.HasThreadID() {
		t.Fatalf("unexpected thread id: %+v", cmd)
	}
}

func TestParseCommandLineMessageThreadID(t *testing.T) {
	cmd, err := ParseCommandLine([]byte(`{"action":"send_text","message_thread_id":456,"text":"hello"}`))
	if err != nil {
		t.Fatalf("ParseCommandLine() error = %v", err)
	}
	if cmd.ThreadID() != 456 || !cmd.HasThreadID() {
		t.Fatalf("unexpected thread id: %+v", cmd)
	}
}

func TestParseCommandLineRejectsConflictingTopicAliases(t *testing.T) {
	if _, err := ParseCommandLine([]byte(`{"action":"send_text","topic":123,"message_thread_id":456,"text":"hello"}`)); err == nil {
		t.Fatal("expected conflicting topic aliases to fail")
	}
}

func TestParseCommandLineRejectsNegativeTopic(t *testing.T) {
	if _, err := ParseCommandLine([]byte(`{"action":"send_text","topic":-1,"text":"hello"}`)); err == nil {
		t.Fatal("expected negative topic to fail")
	}
}

func TestParseCommandLineClickButtonNegativeOffsetRejected(t *testing.T) {
	if _, err := ParseCommandLine([]byte(`{"action":"click_button","button_text":"Open","message_offset":-1}`)); err == nil {
		t.Fatal("expected negative message_offset to fail")
	}
}

func TestParseCommandLineClickButtonMessageText(t *testing.T) {
	cmd, err := ParseCommandLine([]byte(`{"action":"click_button","button_text":"Open","message_text":"Task A"}`))
	if err != nil {
		t.Fatalf("ParseCommandLine() error = %v", err)
	}
	if cmd.ButtonText != "Open" || cmd.MessageText != "Task A" {
		t.Fatalf("unexpected command: %+v", cmd)
	}
}

func TestParseCommandLineEditText(t *testing.T) {
	cmd, err := ParseCommandLine([]byte(`{"action":"edit_text","message_text":"old","text":"new"}`))
	if err != nil {
		t.Fatalf("ParseCommandLine() error = %v", err)
	}
	if cmd.Action != "edit_text" || cmd.MessageText != "old" || cmd.Text != "new" {
		t.Fatalf("unexpected command: %+v", cmd)
	}
}

func TestParseCommandLineEditTextRejectsNegativeOffset(t *testing.T) {
	if _, err := ParseCommandLine([]byte(`{"action":"edit_text","text":"new","message_offset":-1}`)); err == nil {
		t.Fatal("expected negative message_offset to fail")
	}
}

func TestParseCommandLineAssertVisibleText(t *testing.T) {
	cmd, err := ParseCommandLine([]byte(`{"action":"assert_visible_text","text":"done"}`))
	if err != nil {
		t.Fatalf("ParseCommandLine() error = %v", err)
	}
	if cmd.Action != "assert_visible_text" || cmd.Text != "done" {
		t.Fatalf("unexpected command: %+v", cmd)
	}
}

func TestParseCommandLineAssertVisibleTextRequiresText(t *testing.T) {
	if _, err := ParseCommandLine([]byte(`{"action":"assert_visible_text"}`)); err == nil {
		t.Fatal("expected missing text to fail")
	}
}

func TestParseCommandLineDeleteVisibleMessages(t *testing.T) {
	cmd, err := ParseCommandLine([]byte(`{"action":"delete_visible_messages","max_batches":3,"keep_oldest":true}`))
	if err != nil {
		t.Fatalf("ParseCommandLine() error = %v", err)
	}
	if cmd.Action != "delete_visible_messages" || cmd.MaxBatches != 3 || !cmd.KeepOldest {
		t.Fatalf("unexpected command: %+v", cmd)
	}
}

func TestParseCommandLineDeleteVisibleMessagesRejectsNegativeBatchLimit(t *testing.T) {
	if _, err := ParseCommandLine([]byte(`{"action":"delete_visible_messages","max_batches":-1}`)); err == nil {
		t.Fatal("expected negative max_batches to fail")
	}
}

func TestParseCommandLineSelectChatRequiresChat(t *testing.T) {
	if _, err := ParseCommandLine([]byte(`{"action":"select_chat"}`)); err == nil {
		t.Fatal("expected missing chat to fail")
	}
}
