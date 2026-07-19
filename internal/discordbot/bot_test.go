package discordbot

import (
	"strings"
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

func TestSubscriptionListMarksDisabledDelivery(t *testing.T) {
	content := formatSubscriptionList([]database.Subscription{{Status: database.StatusDisabled}})
	if !strings.Contains(content, "전달 불가") {
		t.Fatalf("content=%q", content)
	}
}
