package discordbot

import (
	"testing"

	"github.com/bwmarrin/discordgo"
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
