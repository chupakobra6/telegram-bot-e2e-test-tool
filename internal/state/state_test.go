package state

import "testing"

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
