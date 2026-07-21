package discordbot

import (
	"context"
	"errors"
	"testing"

	"github.com/bwmarrin/discordgo"

	"my-movie/internal/database"
)

func TestBotIgnoresNonCommandInteraction(t *testing.T) {
	bot := &Bot{guildID: "guild"}
	interaction := &discordgo.InteractionCreate{Interaction: &discordgo.Interaction{
		GuildID: "guild",
		Type:    discordgo.InteractionMessageComponent,
		Data:    discordgo.MessageComponentInteractionData{},
	}}

	bot.handleInteraction(nil, interaction)
}

func TestExecuteCommandRoutesAnnouncement(t *testing.T) {
	controller := &fakeController{}
	bot := &Bot{controller: controller}
	data := discordgo.ApplicationCommandInteractionData{Options: []*discordgo.ApplicationCommandInteractionDataOption{{
		Name: "공지", Type: discordgo.ApplicationCommandOptionSubCommand,
		Options: []*discordgo.ApplicationCommandInteractionDataOption{{Name: "내용", Type: discordgo.ApplicationCommandOptionString, Value: "점검 안내"}},
	}}}
	content := bot.executeCommand(context.Background(), "guild", "owner", data)
	if controller.announcedOwner != "owner" || controller.announcedContent != "점검 안내" || content != "공지를 게시했습니다." {
		t.Fatalf("owner=%q announcement=%q response=%q", controller.announcedOwner, controller.announcedContent, content)
	}
}

func TestExecuteJoinRoutesMemberRoleAssignment(t *testing.T) {
	controller := &fakeController{}
	bot := &Bot{controller: controller}
	content := bot.executeJoin(context.Background(), "member")
	if controller.joinedUser != "member" || content != "알림 채널을 열었습니다." {
		t.Fatalf("user=%q response=%q", controller.joinedUser, content)
	}
}

func TestExecuteJoinHidesDiscordErrorDetails(t *testing.T) {
	controller := &fakeController{joinErr: errors.New("HTTP 403 secret details")}
	content := (&Bot{controller: controller}).executeJoin(context.Background(), "member")
	if content != "알림 채널을 열지 못했습니다. 잠시 후 다시 시도해 주세요." {
		t.Fatalf("response=%q", content)
	}
}

type fakeController struct {
	announcedOwner   string
	announcedContent string
	joinedUser       string
	joinErr          error
}

func (c *fakeController) Initialize(context.Context, string, string) (database.Installation, error) {
	return database.Installation{}, nil
}
func (c *fakeController) SelectTarget(context.Context, string, string) error { return nil }
func (c *fakeController) Enable(context.Context, string, string) error       { return nil }
func (c *fakeController) Disable(context.Context, string, string) error      { return nil }
func (c *fakeController) JoinAlerts(_ context.Context, userID string) error {
	c.joinedUser = userID
	return c.joinErr
}
func (c *fakeController) Announce(_ context.Context, ownerID, content string) error {
	c.announcedOwner, c.announcedContent = ownerID, content
	return nil
}
