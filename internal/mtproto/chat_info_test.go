package mtproto

import (
	"testing"

	"github.com/gotd/td/telegram/message/peer"
	"github.com/gotd/td/tg"
)

func TestListedChatsFromEntitiesFiltersGroupsAndQuery(t *testing.T) {
	entities := peer.NewEntities(
		map[int64]*tg.User{
			10: {ID: 10, FirstName: "Plain", Username: "plain_user"},
			11: {ID: 11, FirstName: "Helper", Username: "helper_bot", Bot: true},
		},
		map[int64]*tg.Chat{
			20: {ID: 20, Title: "Legacy group"},
		},
		map[int64]*tg.Channel{
			30: {ID: 30, Title: "TrackMate testing", Megagroup: true, Forum: true},
			31: {ID: 31, Title: "News", Broadcast: true, Username: "news"},
		},
	)

	filtered := filterListedChats(listedChatsFromEntities(entities), ChatListOptions{
		Query:      "trackmate",
		GroupsOnly: true,
	})
	if len(filtered) != 1 {
		t.Fatalf("expected one filtered chat, got %d", len(filtered))
	}
	info := filtered[0].info
	if info.ID != 30 || info.Type != "supergroup" || !info.Forum {
		t.Fatalf("unexpected filtered info: %+v", info)
	}
	if info.Target != "30" {
		t.Fatalf("Target = %q, want 30", info.Target)
	}
	if info.BotAPIID != -1000000000030 {
		t.Fatalf("BotAPIID = %d, want -1000000000030", info.BotAPIID)
	}
}

func TestChatInfoMatchesIDAndUsername(t *testing.T) {
	info := ChatInfo{
		ID:       42,
		BotAPIID: -1000000000042,
		Type:     "supergroup",
		Title:    "TrackMate testing",
		Username: "trackmate_test",
	}

	for _, query := range []string{"trackmate", "TRACKMATE_TEST", "42", "-1000000000042", "supergroup"} {
		if !chatInfoMatches(info, query) {
			t.Fatalf("chatInfoMatches(%q) = false, want true", query)
		}
	}
	if chatInfoMatches(info, "shelfy") {
		t.Fatalf("chatInfoMatches(shelfy) = true, want false")
	}
}

func TestParticipantInfosIncludeBotAndTopicRights(t *testing.T) {
	result := &tg.ChannelsChannelParticipants{
		Participants: []tg.ChannelParticipantClass{
			&tg.ChannelParticipantAdmin{
				UserID:      100,
				AdminRights: tg.ChatAdminRights{ManageTopics: true},
			},
			&tg.ChannelParticipantCreator{UserID: 200},
		},
		Users: []tg.UserClass{
			&tg.User{ID: 100, FirstName: "TrackMate", Username: "trackmate_bot", Bot: true},
			&tg.User{ID: 200, FirstName: "Owner"},
		},
	}

	admins := participantInfos(result)
	if len(admins) != 2 {
		t.Fatalf("expected 2 admins, got %d", len(admins))
	}
	bot := admins[0]
	if bot.ID != 100 || !bot.Bot || bot.Username != "trackmate_bot" || !bot.ManageTopics {
		t.Fatalf("unexpected bot admin info: %+v", bot)
	}
	owner := admins[1]
	if owner.ID != 200 || owner.Role != "creator" || !owner.ManageTopics {
		t.Fatalf("unexpected owner admin info: %+v", owner)
	}
}
