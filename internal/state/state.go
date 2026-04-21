package state

import (
	"fmt"
	"reflect"
	"sort"
	"strings"
	"time"
)

type ChatState struct {
	Target     string           `json:"target"`
	ResolvedAs string           `json:"resolved_as,omitempty"`
	PeerID     int64            `json:"peer_id,omitempty"`
	SyncedAt   time.Time        `json:"synced_at"`
	Messages   []VisibleMessage `json:"messages"`
	Pinned     *PinnedMessage   `json:"pinned,omitempty"`
}

type VisibleMessage struct {
	ID        int              `json:"id"`
	Sender    string           `json:"sender"`
	SenderID  int64            `json:"sender_id,omitempty"`
	Outgoing  bool             `json:"outgoing"`
	Kind      string           `json:"kind"`
	Text      string           `json:"text,omitempty"`
	Pinned    bool             `json:"pinned,omitempty"`
	Timestamp time.Time        `json:"timestamp,omitempty"`
	Buttons   [][]InlineButton `json:"buttons,omitempty"`
}

type InlineButton struct {
	Text         string `json:"text"`
	Kind         string `json:"kind"`
	CallbackData string `json:"callback_data,omitempty"`
}

type PinnedMessage struct {
	MessageID int              `json:"message_id"`
	Text      string           `json:"text,omitempty"`
	Buttons   [][]InlineButton `json:"buttons,omitempty"`
}

type ChatDiff struct {
	Added         []int  `json:"added,omitempty"`
	Removed       []int  `json:"removed,omitempty"`
	Changed       []int  `json:"changed,omitempty"`
	PinnedChanged bool   `json:"pinned_changed,omitempty"`
	Summary       string `json:"summary,omitempty"`
}

func Diff(prev, next ChatState) ChatDiff {
	prevMessages := map[int]VisibleMessage{}
	nextMessages := map[int]VisibleMessage{}
	for _, msg := range prev.Messages {
		prevMessages[msg.ID] = msg
	}
	for _, msg := range next.Messages {
		nextMessages[msg.ID] = msg
	}

	diff := ChatDiff{}
	for id, msg := range nextMessages {
		prevMsg, ok := prevMessages[id]
		if !ok {
			diff.Added = append(diff.Added, id)
			continue
		}
		if !reflect.DeepEqual(prevMsg, msg) {
			diff.Changed = append(diff.Changed, id)
		}
	}
	for id := range prevMessages {
		if _, ok := nextMessages[id]; !ok {
			diff.Removed = append(diff.Removed, id)
		}
	}
	diff.PinnedChanged = !reflect.DeepEqual(prev.Pinned, next.Pinned)
	sort.Ints(diff.Added)
	sort.Ints(diff.Removed)
	sort.Ints(diff.Changed)
	diff.Summary = diffSummary(diff)
	return diff
}

func (d ChatDiff) HasChanges() bool {
	return len(d.Added) > 0 || len(d.Removed) > 0 || len(d.Changed) > 0 || d.PinnedChanged
}

func diffSummary(d ChatDiff) string {
	parts := make([]string, 0, 4)
	if len(d.Added) > 0 {
		parts = append(parts, fmt.Sprintf("added=%d", len(d.Added)))
	}
	if len(d.Changed) > 0 {
		parts = append(parts, fmt.Sprintf("changed=%d", len(d.Changed)))
	}
	if len(d.Removed) > 0 {
		parts = append(parts, fmt.Sprintf("removed=%d", len(d.Removed)))
	}
	if d.PinnedChanged {
		parts = append(parts, "pinned=changed")
	}
	if len(parts) == 0 {
		return "no visible changes"
	}
	return strings.Join(parts, ", ")
}
