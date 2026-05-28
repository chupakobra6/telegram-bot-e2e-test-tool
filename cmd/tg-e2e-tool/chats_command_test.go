package main

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/igor/telegram-bot-e2e-test-tool/internal/mtproto"
)

func TestWriteChatsTextIncludesTargetsAndTopics(t *testing.T) {
	var out bytes.Buffer
	writeChatsText(&out, []mtproto.ChatInfo{
		{
			ID:         123,
			Target:     "123",
			BotAPIID:   -1000000000123,
			Type:       "supergroup",
			Title:      "TrackMate testing",
			Forum:      true,
			TopicCount: 2,
			Topics: []mtproto.ForumTopicInfo{
				{ID: 1, Title: "General", Hidden: true},
				{ID: 77, Title: "Сегодня", TopMessage: 77},
			},
			AdminCount: 1,
			Admins: []mtproto.ParticipantInfo{
				{ID: 456, DisplayName: "TrackMate", Username: "trackmate_bot", Bot: true, Role: "admin", ManageTopics: true},
			},
		},
	})

	body := out.String()
	for _, want := range []string{
		`target=123`,
		`bot_api_id=-1000000000123`,
		`title="TrackMate testing"`,
		`forum=true`,
		`topic_count=2`,
		`topic_id=77 title="Сегодня" top_message=77`,
		`admin_count=1`,
		`admin_id=456 role=admin name="TrackMate" username=@trackmate_bot bot=true manage_topics=true`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("output missing %q:\n%s", want, body)
		}
	}
}

func TestWriteChatsJSON(t *testing.T) {
	var out bytes.Buffer
	chats := []mtproto.ChatInfo{{ID: 123, Target: "123", Type: "supergroup", Title: "TrackMate"}}
	if err := writeChatsJSON(&out, chats); err != nil {
		t.Fatalf("writeChatsJSON() error = %v", err)
	}
	var decoded []mtproto.ChatInfo
	if err := json.Unmarshal(out.Bytes(), &decoded); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	if len(decoded) != 1 || decoded[0].Target != "123" {
		t.Fatalf("decoded = %+v", decoded)
	}
}

func TestWriteChatsTextEmpty(t *testing.T) {
	var out bytes.Buffer
	writeChatsText(&out, nil)
	if got := strings.TrimSpace(out.String()); got != "no chats found" {
		t.Fatalf("empty output = %q", got)
	}
}
