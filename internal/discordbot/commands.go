package discordbot

import "github.com/bwmarrin/discordgo"

func Command() *discordgo.ApplicationCommand {
	return &discordgo.ApplicationCommand{
		Name: "알림", Description: "영화 예매 오픈 알림을 관리합니다",
		Options: []*discordgo.ApplicationCommandOption{
			{Type: discordgo.ApplicationCommandOptionSubCommand, Name: "초기화", Description: "비공개 알림 채널과 제어 패널을 만듭니다"},
		},
	}
}
