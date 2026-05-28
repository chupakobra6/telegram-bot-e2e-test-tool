package state

import (
	"encoding/json"
	"testing"
)

func TestDiff(t *testing.T) {
	prev := ChatState{
		Messages: []VisibleMessage{
			{ID: 1, Text: "old"},
			{ID: 2, Text: "same"},
		},
		Pinned: &PinnedMessage{MessageID: 1, Text: "old"},
	}
	next := ChatState{
		Messages: []VisibleMessage{
			{ID: 2, Text: "same"},
			{ID: 3, Text: "new"},
			{ID: 1, Text: "updated"},
		},
		Pinned: &PinnedMessage{MessageID: 3, Text: "new"},
	}

	diff := Diff(prev, next)
	if !diff.HasChanges() {
		t.Fatal("expected diff to contain changes")
	}
	if len(diff.Added) != 1 || diff.Added[0] != 3 {
		t.Fatalf("unexpected added: %+v", diff.Added)
	}
	if len(diff.Removed) != 0 {
		t.Fatalf("unexpected removed: %+v", diff.Removed)
	}
	if len(diff.Changed) != 1 || diff.Changed[0] != 1 {
		t.Fatalf("unexpected changed: %+v", diff.Changed)
	}
	if !diff.PinnedChanged {
		t.Fatal("expected pinned change")
	}
}

func TestChatStateIncludesMessageThreadID(t *testing.T) {
	body, err := json.Marshal(ChatState{
		Target:          "@group",
		MessageThreadID: 123,
		Messages:        []VisibleMessage{},
	})
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}
	if string(body) != `{"target":"@group","message_thread_id":123,"synced_at":"0001-01-01T00:00:00Z","messages":[]}` {
		t.Fatalf("unexpected JSON: %s", body)
	}
}
