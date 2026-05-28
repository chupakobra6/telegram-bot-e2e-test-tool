package mtproto

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/gotd/td/telegram/message/peer"
	"github.com/gotd/td/tg"
)

const defaultForumTopicLimit = 100

type ChatListOptions struct {
	Query         string
	GroupsOnly    bool
	IncludeTopics bool
	IncludeAdmins bool
	TopicLimit    int
}

type ChatInfo struct {
	ID         int64             `json:"id"`
	Target     string            `json:"target"`
	BotAPIID   int64             `json:"bot_api_id,omitempty"`
	Type       string            `json:"type"`
	Title      string            `json:"title,omitempty"`
	Username   string            `json:"username,omitempty"`
	Bot        bool              `json:"bot,omitempty"`
	Forum      bool              `json:"forum,omitempty"`
	Megagroup  bool              `json:"megagroup,omitempty"`
	Broadcast  bool              `json:"broadcast,omitempty"`
	TopicCount int               `json:"topic_count,omitempty"`
	Topics     []ForumTopicInfo  `json:"topics,omitempty"`
	AdminCount int               `json:"admin_count,omitempty"`
	Admins     []ParticipantInfo `json:"admins,omitempty"`
}

type ForumTopicInfo struct {
	ID          int    `json:"id"`
	Title       string `json:"title,omitempty"`
	Closed      bool   `json:"closed,omitempty"`
	Pinned      bool   `json:"pinned,omitempty"`
	Hidden      bool   `json:"hidden,omitempty"`
	TopMessage  int    `json:"top_message,omitempty"`
	UnreadCount int    `json:"unread_count,omitempty"`
}

type ParticipantInfo struct {
	ID           int64  `json:"id"`
	Username     string `json:"username,omitempty"`
	DisplayName  string `json:"display_name,omitempty"`
	Bot          bool   `json:"bot,omitempty"`
	Role         string `json:"role"`
	ManageTopics bool   `json:"manage_topics,omitempty"`
	Self         bool   `json:"self,omitempty"`
}

type listedChat struct {
	info         ChatInfo
	inputPeer    tg.InputPeerClass
	inputChannel tg.InputChannelClass
}

func (s *Session) ListChats(ctx context.Context, opts ChatListOptions) ([]ChatInfo, error) {
	entities, err := s.loadAllDialogEntities(ctx)
	if err != nil {
		return nil, err
	}

	listed := filterListedChats(listedChatsFromEntities(entities), opts)
	if opts.IncludeTopics {
		topicLimit := opts.TopicLimit
		if topicLimit <= 0 {
			topicLimit = defaultForumTopicLimit
		}
		for i := range listed {
			if !listed[i].info.Forum {
				continue
			}
			topics, count, err := s.listForumTopics(ctx, listed[i].inputPeer, topicLimit)
			if err != nil {
				return nil, fmt.Errorf("load forum topics for %q (%d): %w", listed[i].info.Title, listed[i].info.ID, err)
			}
			listed[i].info.TopicCount = count
			listed[i].info.Topics = topics
		}
	}
	if opts.IncludeAdmins {
		for i := range listed {
			if listed[i].inputChannel == nil {
				continue
			}
			admins, count, err := s.listChannelAdmins(ctx, listed[i].inputChannel)
			if err != nil {
				return nil, fmt.Errorf("load admins for %q (%d): %w", listed[i].info.Title, listed[i].info.ID, err)
			}
			listed[i].info.AdminCount = count
			listed[i].info.Admins = admins
		}
	}

	chats := make([]ChatInfo, 0, len(listed))
	for _, chat := range listed {
		chats = append(chats, chat.info)
	}
	return chats, nil
}

func listedChatsFromEntities(entities peer.Entities) []listedChat {
	chats := make([]listedChat, 0, len(entities.Users())+len(entities.Chats())+len(entities.Channels()))
	for _, user := range entities.Users() {
		title := strings.TrimSpace(user.FirstName + " " + user.LastName)
		if title == "" {
			title = user.Username
		}
		chatType := "user"
		if user.Bot {
			chatType = "bot"
		}
		info := ChatInfo{
			ID:       user.ID,
			Target:   strconv.FormatInt(user.ID, 10),
			Type:     chatType,
			Title:    title,
			Username: user.Username,
			Bot:      user.Bot,
		}
		chats = append(chats, listedChat{info: info, inputPeer: user.AsInputPeer()})
	}
	for _, chat := range entities.Chats() {
		id := chat.ID
		info := ChatInfo{
			ID:       id,
			Target:   strconv.FormatInt(id, 10),
			BotAPIID: -id,
			Type:     "group",
			Title:    chat.Title,
		}
		chats = append(chats, listedChat{info: info, inputPeer: &tg.InputPeerChat{ChatID: id}})
	}
	for _, channel := range entities.Channels() {
		chatType := "channel"
		if channel.Megagroup {
			chatType = "supergroup"
		}
		info := ChatInfo{
			ID:        channel.ID,
			Target:    strconv.FormatInt(channel.ID, 10),
			BotAPIID:  botAPIChannelID(channel.ID),
			Type:      chatType,
			Title:     channel.Title,
			Username:  channel.Username,
			Forum:     channel.Forum,
			Megagroup: channel.Megagroup,
			Broadcast: channel.Broadcast,
		}
		inputChannel := &tg.InputChannel{ChannelID: channel.ID, AccessHash: channel.AccessHash}
		chats = append(chats, listedChat{
			info:         info,
			inputPeer:    &tg.InputPeerChannel{ChannelID: channel.ID, AccessHash: channel.AccessHash},
			inputChannel: inputChannel,
		})
	}
	sort.Slice(chats, func(i, j int) bool {
		left := strings.ToLower(chats[i].info.Title)
		right := strings.ToLower(chats[j].info.Title)
		if left != right {
			return left < right
		}
		if chats[i].info.Type != chats[j].info.Type {
			return chats[i].info.Type < chats[j].info.Type
		}
		return chats[i].info.ID < chats[j].info.ID
	})
	return chats
}

func filterListedChats(chats []listedChat, opts ChatListOptions) []listedChat {
	filtered := make([]listedChat, 0, len(chats))
	for _, chat := range chats {
		if opts.GroupsOnly && chat.info.Type != "group" && chat.info.Type != "supergroup" {
			continue
		}
		if !chatInfoMatches(chat.info, opts.Query) {
			continue
		}
		filtered = append(filtered, chat)
	}
	return filtered
}

func chatInfoMatches(chat ChatInfo, query string) bool {
	query = strings.ToLower(strings.TrimSpace(query))
	if query == "" {
		return true
	}
	haystack := strings.ToLower(strings.Join([]string{
		chat.Title,
		chat.Username,
		chat.Type,
		strconv.FormatInt(chat.ID, 10),
		strconv.FormatInt(chat.BotAPIID, 10),
	}, " "))
	return strings.Contains(haystack, query)
}

func (s *Session) listForumTopics(ctx context.Context, inputPeer tg.InputPeerClass, limit int) ([]ForumTopicInfo, int, error) {
	var result *tg.MessagesForumTopics
	err := s.performRPC(ctx, "get_forum_topics", false, func(callCtx context.Context) error {
		var callErr error
		result, callErr = s.raw.MessagesGetForumTopics(callCtx, &tg.MessagesGetForumTopicsRequest{
			Peer:        inputPeer,
			OffsetDate:  0,
			OffsetID:    0,
			OffsetTopic: 0,
			Limit:       limit,
		})
		return callErr
	})
	if err != nil {
		return nil, 0, err
	}
	if result == nil {
		return nil, 0, nil
	}
	topics := make([]ForumTopicInfo, 0, len(result.Topics))
	for _, topicClass := range result.Topics {
		topic, ok := topicClass.(*tg.ForumTopic)
		if !ok {
			continue
		}
		topics = append(topics, ForumTopicInfo{
			ID:          topic.ID,
			Title:       topic.Title,
			Closed:      topic.Closed,
			Pinned:      topic.Pinned,
			Hidden:      topic.Hidden,
			TopMessage:  topic.TopMessage,
			UnreadCount: topic.UnreadCount,
		})
	}
	sort.Slice(topics, func(i, j int) bool { return topics[i].ID < topics[j].ID })
	return topics, result.Count, nil
}

func (s *Session) listChannelAdmins(ctx context.Context, inputChannel tg.InputChannelClass) ([]ParticipantInfo, int, error) {
	var result tg.ChannelsChannelParticipantsClass
	err := s.performRPC(ctx, "get_participants", false, func(callCtx context.Context) error {
		var callErr error
		result, callErr = s.raw.ChannelsGetParticipants(callCtx, &tg.ChannelsGetParticipantsRequest{
			Channel: inputChannel,
			Filter:  &tg.ChannelParticipantsAdmins{},
			Offset:  0,
			Limit:   100,
			Hash:    0,
		})
		return callErr
	})
	if err != nil {
		return nil, 0, err
	}
	modified, ok := result.AsModified()
	if !ok {
		return nil, 0, nil
	}
	return participantInfos(modified), modified.Count, nil
}

func participantInfos(result *tg.ChannelsChannelParticipants) []ParticipantInfo {
	if result == nil {
		return nil
	}
	users := tg.UserClassArray(result.Users).UserToMap()
	participants := make([]ParticipantInfo, 0, len(result.Participants))
	for _, participant := range result.Participants {
		userID, role, rights, self, ok := channelParticipantDetails(participant)
		if !ok {
			continue
		}
		user := users[userID]
		info := ParticipantInfo{
			ID:           userID,
			Role:         role,
			ManageTopics: rights.ManageTopics || role == "creator",
			Self:         self,
		}
		if user != nil {
			info.Username = user.Username
			info.DisplayName = userDisplayName(user)
			info.Bot = user.Bot
		}
		participants = append(participants, info)
	}
	sort.Slice(participants, func(i, j int) bool {
		if participants[i].Bot != participants[j].Bot {
			return participants[i].Bot
		}
		left := strings.ToLower(participants[i].DisplayName + participants[i].Username)
		right := strings.ToLower(participants[j].DisplayName + participants[j].Username)
		if left != right {
			return left < right
		}
		return participants[i].ID < participants[j].ID
	})
	return participants
}

func channelParticipantDetails(participant tg.ChannelParticipantClass) (int64, string, tg.ChatAdminRights, bool, bool) {
	switch typed := participant.(type) {
	case *tg.ChannelParticipantCreator:
		return typed.UserID, "creator", typed.AdminRights, false, true
	case *tg.ChannelParticipantAdmin:
		return typed.UserID, "admin", typed.AdminRights, typed.Self, true
	case *tg.ChannelParticipantSelf:
		return typed.UserID, "self", tg.ChatAdminRights{}, true, true
	case *tg.ChannelParticipant:
		return typed.UserID, "member", tg.ChatAdminRights{}, false, true
	default:
		return 0, "", tg.ChatAdminRights{}, false, false
	}
}

func userDisplayName(user *tg.User) string {
	if user == nil {
		return ""
	}
	name := strings.TrimSpace(user.FirstName + " " + user.LastName)
	if name != "" {
		return name
	}
	return user.Username
}

func botAPIChannelID(id int64) int64 {
	if id <= 0 {
		return 0
	}
	return -1000000000000 - id
}
