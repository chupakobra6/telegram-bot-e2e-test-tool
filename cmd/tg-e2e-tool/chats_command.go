package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/igor/telegram-bot-e2e-test-tool/internal/config"
	"github.com/igor/telegram-bot-e2e-test-tool/internal/mtproto"
)

func runChatsCommand(cfg config.Config, client interface {
	RunAuthorized(context.Context, func(context.Context, *mtproto.Session) error) error
}, args []string, out io.Writer) error {
	fs := flag.NewFlagSet("chats", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	filter := fs.String("filter", "", "case-insensitive substring filter for title, username, type, or id")
	fs.StringVar(filter, "query", "", "alias for --filter")
	groupsOnly := fs.Bool("groups", false, "show only groups and supergroups")
	includeTopics := fs.Bool("topics", false, "include forum topics for forum supergroups")
	includeAdmins := fs.Bool("admins", false, "include channel/supergroup admins and their topic-management rights")
	topicLimit := fs.Int("topic-limit", 100, "maximum number of forum topics to load per forum group")
	jsonOut := fs.Bool("json", false, "print JSON instead of text")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *topicLimit < 1 {
		return fmt.Errorf("--topic-limit must be > 0")
	}

	opts := mtproto.ChatListOptions{
		Query:         *filter,
		GroupsOnly:    *groupsOnly,
		IncludeTopics: *includeTopics,
		IncludeAdmins: *includeAdmins,
		TopicLimit:    *topicLimit,
	}
	return withRuntimeLock(cfg, func() error {
		return client.RunAuthorized(context.Background(), func(ctx context.Context, session *mtproto.Session) error {
			chats, err := session.ListChats(ctx, opts)
			if err != nil {
				return err
			}
			if *jsonOut {
				return writeChatsJSON(out, chats)
			}
			writeChatsText(out, chats)
			return nil
		})
	})
}

func writeChatsJSON(out io.Writer, chats []mtproto.ChatInfo) error {
	encoder := json.NewEncoder(out)
	encoder.SetIndent("", "  ")
	return encoder.Encode(chats)
}

func writeChatsText(out io.Writer, chats []mtproto.ChatInfo) {
	if len(chats) == 0 {
		fmt.Fprintln(out, "no chats found")
		return
	}
	for _, chat := range chats {
		attrs := []string{
			fmt.Sprintf("target=%s", chat.Target),
			fmt.Sprintf("id=%d", chat.ID),
			fmt.Sprintf("type=%s", chat.Type),
		}
		if chat.BotAPIID != 0 {
			attrs = append(attrs, fmt.Sprintf("bot_api_id=%d", chat.BotAPIID))
		}
		if chat.Title != "" {
			attrs = append(attrs, fmt.Sprintf("title=%q", chat.Title))
		}
		if chat.Username != "" {
			attrs = append(attrs, fmt.Sprintf("username=@%s", strings.TrimPrefix(chat.Username, "@")))
		}
		if chat.Forum {
			attrs = append(attrs, "forum=true")
		}
		if chat.TopicCount > 0 {
			attrs = append(attrs, fmt.Sprintf("topic_count=%d", chat.TopicCount))
		}
		if chat.AdminCount > 0 {
			attrs = append(attrs, fmt.Sprintf("admin_count=%d", chat.AdminCount))
		}
		fmt.Fprintln(out, strings.Join(attrs, " "))
		for _, topic := range chat.Topics {
			topicAttrs := []string{
				fmt.Sprintf("topic_id=%d", topic.ID),
			}
			if topic.Title != "" {
				topicAttrs = append(topicAttrs, fmt.Sprintf("title=%q", topic.Title))
			}
			if topic.Closed {
				topicAttrs = append(topicAttrs, "closed=true")
			}
			if topic.Pinned {
				topicAttrs = append(topicAttrs, "pinned=true")
			}
			if topic.Hidden {
				topicAttrs = append(topicAttrs, "hidden=true")
			}
			if topic.TopMessage != 0 {
				topicAttrs = append(topicAttrs, fmt.Sprintf("top_message=%d", topic.TopMessage))
			}
			if topic.UnreadCount != 0 {
				topicAttrs = append(topicAttrs, fmt.Sprintf("unread_count=%d", topic.UnreadCount))
			}
			fmt.Fprintf(out, "  %s\n", strings.Join(topicAttrs, " "))
		}
		for _, admin := range chat.Admins {
			adminAttrs := []string{
				fmt.Sprintf("admin_id=%d", admin.ID),
				fmt.Sprintf("role=%s", admin.Role),
			}
			if admin.DisplayName != "" {
				adminAttrs = append(adminAttrs, fmt.Sprintf("name=%q", admin.DisplayName))
			}
			if admin.Username != "" {
				adminAttrs = append(adminAttrs, fmt.Sprintf("username=@%s", strings.TrimPrefix(admin.Username, "@")))
			}
			if admin.Bot {
				adminAttrs = append(adminAttrs, "bot=true")
			}
			if admin.ManageTopics {
				adminAttrs = append(adminAttrs, "manage_topics=true")
			}
			if admin.Self {
				adminAttrs = append(adminAttrs, "self=true")
			}
			fmt.Fprintf(out, "  %s\n", strings.Join(adminAttrs, " "))
		}
	}
}
