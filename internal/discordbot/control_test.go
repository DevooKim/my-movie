package discordbot

import (
	"strings"
	"testing"

	"github.com/bwmarrin/discordgo"

	"my-movie/internal/control"
)

func TestCommandExposesOnlyInitialization(t *testing.T) {
	command := Command()
	if command.Name != "알림" || len(command.Options) != 1 || command.Options[0].Name != "초기화" {
		t.Fatalf("command=%+v", command)
	}
	if command.Options[0].Type != discordgo.ApplicationCommandOptionSubCommand {
		t.Fatalf("option type=%v", command.Options[0].Type)
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
	for _, overwrite := range overwrites[1:] {
		if overwrite.Type != discordgo.PermissionOverwriteTypeMember || overwrite.Allow&discordgo.PermissionViewChannel == 0 || overwrite.Allow&discordgo.PermissionSendMessages == 0 {
			t.Fatalf("member=%+v", overwrite)
		}
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
	botAllow := int64(discordgo.PermissionViewChannel | discordgo.PermissionReadMessageHistory | discordgo.PermissionSendMessages | discordgo.PermissionManageChannels)
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
