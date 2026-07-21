package discordbot

import (
	"strings"
	"testing"

	"github.com/bwmarrin/discordgo"

	"my-movie/internal/control"
)

func TestCommandExposesInitializationAndOwnerAnnouncement(t *testing.T) {
	command := Command()
	if command.Name != "알림" || len(command.Options) != 2 || command.Options[0].Name != "초기화" || command.Options[1].Name != "공지" {
		t.Fatalf("command=%+v", command)
	}
	announcement := command.Options[1]
	if announcement.Type != discordgo.ApplicationCommandOptionSubCommand || len(announcement.Options) != 1 || announcement.Options[0].Name != "내용" || announcement.Options[0].Type != discordgo.ApplicationCommandOptionString || !announcement.Options[0].Required {
		t.Fatalf("announcement=%+v", announcement)
	}
}

func TestPanelUsesPlainMarkdownSelectAndTargetSpecificButtons(t *testing.T) {
	message := panelMessage(control.Panel{
		SelectedTargetID: "cgv-yongsan-imax",
		Targets: []control.TargetStatus{
			{ID: "cgv-yongsan-imax", Name: "CGV 용산아이파크몰 · IMAX", Enabled: true},
			{ID: "cgv-yongsan-4dx", Name: "CGV 용산아이파크몰 · 4DX", Enabled: false},
		},
	})
	if len(message.Embeds) != 0 || !strings.Contains(message.Content, "CGV 용산아이파크몰 · IMAX: ON") || !strings.Contains(message.Content, "CGV 용산아이파크몰 · 4DX: OFF") {
		t.Fatalf("message=%+v", message)
	}
	if len(message.Components) != 2 {
		t.Fatalf("components=%+v", message.Components)
	}
	buttons := message.Components[1].(discordgo.ActionsRow).Components
	if buttons[0].(discordgo.Button).CustomID != "alerts:enable:cgv-yongsan-imax" || buttons[1].(discordgo.Button).CustomID != "alerts:disable:cgv-yongsan-imax" {
		t.Fatalf("buttons=%+v", buttons)
	}
}

func TestPrivateOverwritesHideChannelsFromEveryone(t *testing.T) {
	overwrites := privateOverwrites("guild", "owner", "bot")
	if len(overwrites) != 3 {
		t.Fatalf("overwrites=%+v", overwrites)
	}
	if overwrites[0].ID != "guild" || overwrites[0].Type != discordgo.PermissionOverwriteTypeRole || overwrites[0].Deny&discordgo.PermissionViewChannel == 0 || overwrites[0].Deny&discordgo.PermissionSendMessages == 0 {
		t.Fatalf("everyone=%+v", overwrites[0])
	}
	owner := overwrites[1]
	if owner.Type != discordgo.PermissionOverwriteTypeMember || owner.Allow&discordgo.PermissionViewChannel == 0 || owner.Allow&discordgo.PermissionReadMessageHistory == 0 || owner.Allow&discordgo.PermissionSendMessages != 0 {
		t.Fatalf("owner=%+v", owner)
	}
	bot := overwrites[2]
	if bot.Type != discordgo.PermissionOverwriteTypeMember || bot.Allow&discordgo.PermissionSendMessages == 0 {
		t.Fatalf("bot=%+v", bot)
	}
}

func TestRestrictedOverwritesExposeReadOnlyChannelsOnlyToViewerRole(t *testing.T) {
	overwrites := restrictedOverwrites("guild", "viewer", "bot")
	if len(overwrites) != 3 {
		t.Fatalf("overwrites=%+v", overwrites)
	}
	everyone := overwrites[0]
	if everyone.ID != "guild" || everyone.Deny&discordgo.PermissionViewChannel == 0 || everyone.Deny&discordgo.PermissionSendMessages == 0 {
		t.Fatalf("everyone=%+v", everyone)
	}
	viewer := overwrites[1]
	viewerAllow := int64(discordgo.PermissionViewChannel | discordgo.PermissionReadMessageHistory | discordgo.PermissionAddReactions)
	viewerDeny := int64(discordgo.PermissionSendMessages | discordgo.PermissionCreatePublicThreads | discordgo.PermissionCreatePrivateThreads | discordgo.PermissionSendMessagesInThreads)
	if viewer.ID != "viewer" || viewer.Type != discordgo.PermissionOverwriteTypeRole || viewer.Allow&viewerAllow != viewerAllow || viewer.Deny&viewerDeny != viewerDeny {
		t.Fatalf("viewer=%+v", viewer)
	}
	bot := overwrites[2]
	if bot.ID != "bot" || bot.Allow&discordgo.PermissionSendMessages == 0 || bot.Allow&discordgo.PermissionManageChannels == 0 {
		t.Fatalf("bot=%+v", bot)
	}
}

func TestGuideImageMessageContainsOnlyImage(t *testing.T) {
	message := guideImageMessage()
	if message.Content != "" || len(message.Components) != 0 || len(message.Files) != 1 || message.Files[0].Name != "my-movie-pepe.png" {
		t.Fatalf("message=%+v", message)
	}
}

func TestGuideMessageIncludesPlatformNeutralCopyAndJoinButton(t *testing.T) {
	message := guideMessage()
	if strings.Contains(strings.ToLower(message.Content), "iphone") || strings.Contains(message.Content, "아이폰") {
		t.Fatalf("platform-specific content=%q", message.Content)
	}
	if !strings.Contains(message.Content, "CGV와 메가박스") || len(message.Files) != 0 {
		t.Fatalf("message=%+v", message)
	}
	button := message.Components[0].(discordgo.ActionsRow).Components[0].(discordgo.Button)
	if button.CustomID != "alerts:join" || button.Label != "🔔 알림 채널 보기" {
		t.Fatalf("button=%+v", button)
	}
}

func TestViewerRoleMustBeUnmanagedUnprivilegedAndNotAboveBot(t *testing.T) {
	tests := []struct {
		name string
		role *discordgo.Role
		want bool
	}{
		{name: "safe", role: &discordgo.Role{ID: "viewer", Position: 1}, want: true},
		{name: "same position as bot", role: &discordgo.Role{ID: "viewer", Position: 2}, want: true},
		{name: "administrator", role: &discordgo.Role{ID: "viewer", Position: 1, Permissions: discordgo.PermissionAdministrator}},
		{name: "managed", role: &discordgo.Role{ID: "viewer", Position: 1, Managed: true}},
		{name: "above bot", role: &discordgo.Role{ID: "viewer", Position: 3}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := isSafeViewerRole(test.role, 2); got != test.want {
				t.Fatalf("safe=%v want=%v role=%+v", got, test.want, test.role)
			}
		})
	}
}

func TestGuideMessageCanBeRecoveredOnlyFromBotAuthoredJoinButton(t *testing.T) {
	guide := &discordgo.Message{Author: &discordgo.User{ID: "bot"}, Components: []discordgo.MessageComponent{
		discordgo.ActionsRow{Components: []discordgo.MessageComponent{discordgo.Button{CustomID: "alerts:join"}}},
	}}
	if !isGuideMessage(guide, "bot") {
		t.Fatal("bot guide message was not recognized")
	}
	guide.Author.ID = "member"
	if isGuideMessage(guide, "bot") {
		t.Fatal("member-authored message was accepted")
	}
}

func TestGuideImageMessageCanBeRecoveredOnlyFromBotAuthoredImageOnlyMessage(t *testing.T) {
	messages := []*discordgo.Message{
		{ID: "combined", Author: &discordgo.User{ID: "bot"}, Content: "안내", Attachments: []*discordgo.MessageAttachment{{Filename: "my-movie-pepe.png"}}},
		{ID: "member-image", Author: &discordgo.User{ID: "member"}, Attachments: []*discordgo.MessageAttachment{{Filename: "my-movie-pepe.png"}}},
		{ID: "image", Author: &discordgo.User{ID: "bot"}, Attachments: []*discordgo.MessageAttachment{{Filename: "my-movie-pepe.png"}}},
	}
	if got := findGuideImageMessage(messages, "bot"); got == nil || got.ID != "image" {
		t.Fatalf("image=%+v", got)
	}
}

func TestFallbackMessageIDUsesRecoveredCandidateAfterStoredIDFails(t *testing.T) {
	recovered := &discordgo.Message{ID: "200"}
	if got := fallbackMessageID("100", recovered); got != "200" {
		t.Fatalf("fallback=%q", got)
	}
	if got := fallbackMessageID("200", recovered); got != "" {
		t.Fatalf("same failed candidate should not retry: %q", got)
	}
}

func TestMessagePredatesUsesDiscordSnowflakeOrder(t *testing.T) {
	if !messagePredates("100", "200") {
		t.Fatal("message 100 should predate message 200")
	}
	if messagePredates("200", "100") || messagePredates("invalid", "200") {
		t.Fatal("newer or invalid message ID should not predate image")
	}
}

func TestFindGuideMessageAfterImageRecoversUnpersistedReplacement(t *testing.T) {
	guide := func(id string) *discordgo.Message {
		return &discordgo.Message{ID: id, Author: &discordgo.User{ID: "bot"}, Components: []discordgo.MessageComponent{
			discordgo.ActionsRow{Components: []discordgo.MessageComponent{discordgo.Button{CustomID: "alerts:join"}}},
		}}
	}
	messages := []*discordgo.Message{guide("300"), guide("100")}
	if got := findGuideMessageAfterImage(messages, "bot", "200"); got == nil || got.ID != "300" {
		t.Fatalf("guide=%+v", got)
	}
}

func TestPublicOverwritesAllowReadingAndReactionsButDenyWriting(t *testing.T) {
	overwrites := publicOverwrites("guild", "bot")
	if len(overwrites) != 2 {
		t.Fatalf("overwrites=%+v", overwrites)
	}
	everyone := overwrites[0]
	memberAllow := int64(discordgo.PermissionViewChannel | discordgo.PermissionReadMessageHistory | discordgo.PermissionAddReactions)
	memberDeny := int64(discordgo.PermissionSendMessages | discordgo.PermissionCreatePublicThreads | discordgo.PermissionCreatePrivateThreads | discordgo.PermissionSendMessagesInThreads)
	if everyone.ID != "guild" || everyone.Type != discordgo.PermissionOverwriteTypeRole || everyone.Allow&memberAllow != memberAllow || everyone.Deny&memberDeny != memberDeny {
		t.Fatalf("everyone=%+v", everyone)
	}
	bot := overwrites[1]
	botAllow := int64(discordgo.PermissionViewChannel | discordgo.PermissionReadMessageHistory | discordgo.PermissionSendMessages | discordgo.PermissionManageChannels | discordgo.PermissionCreatePublicThreads | discordgo.PermissionCreatePrivateThreads | discordgo.PermissionSendMessagesInThreads)
	if bot.ID != "bot" || bot.Type != discordgo.PermissionOverwriteTypeMember || bot.Allow&botAllow != botAllow {
		t.Fatalf("bot=%+v", bot)
	}
}

func TestParseComponentActionRejectsMalformedIDs(t *testing.T) {
	for _, raw := range []string{"", "alerts:enable", "other:enable:x", "alerts:unknown:x"} {
		if _, _, ok := parseComponentAction(raw); ok {
			t.Fatalf("accepted %q", raw)
		}
	}
	action, targetID, ok := parseComponentAction("alerts:disable:cgv-yongsan-imax")
	if !ok || action != "disable" || targetID != "cgv-yongsan-imax" {
		t.Fatalf("action=%q target=%q ok=%v", action, targetID, ok)
	}
}
