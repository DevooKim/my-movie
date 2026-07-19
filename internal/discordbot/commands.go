package discordbot

import (
	"github.com/bwmarrin/discordgo"

	"my-movie/internal/providers"
)

func Command(registry *providers.Registry) *discordgo.ApplicationCommand {
	providerChoices := make([]*discordgo.ApplicationCommandOptionChoice, 0, len(registry.IDs()))
	for _, id := range registry.IDs() {
		providerChoices = append(providerChoices, &discordgo.ApplicationCommandOptionChoice{Name: providerName(id), Value: string(id)})
	}
	return &discordgo.ApplicationCommand{
		Name: "알림", Description: "영화 예매 오픈 알림을 관리합니다",
		Options: []*discordgo.ApplicationCommandOption{
			{Type: discordgo.ApplicationCommandOptionSubCommand, Name: "등록", Description: "영화와 지점의 오픈 알림을 등록합니다", Options: []*discordgo.ApplicationCommandOption{
				{Type: discordgo.ApplicationCommandOptionString, Name: "영화관", Description: "영화관", Required: true, Choices: providerChoices},
				{Type: discordgo.ApplicationCommandOptionString, Name: "지점", Description: "알림 받을 지점", Required: true, Autocomplete: true},
				{Type: discordgo.ApplicationCommandOptionString, Name: "영화", Description: "알림 받을 영화", Required: true, Autocomplete: true},
			}},
			{Type: discordgo.ApplicationCommandOptionSubCommand, Name: "목록", Description: "등록된 알림을 확인합니다"},
			{Type: discordgo.ApplicationCommandOptionSubCommand, Name: "삭제", Description: "알림 하나를 삭제합니다", Options: []*discordgo.ApplicationCommandOption{
				{Type: discordgo.ApplicationCommandOptionString, Name: "알림", Description: "삭제할 알림", Required: true, Autocomplete: true},
			}},
			{Type: discordgo.ApplicationCommandOptionSubCommand, Name: "전체삭제", Description: "모든 알림을 삭제합니다"},
			{Type: discordgo.ApplicationCommandOptionSubCommand, Name: "도움말", Description: "사용법을 확인합니다"},
		},
	}
}
