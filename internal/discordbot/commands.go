package discordbot

import "github.com/bwmarrin/discordgo"

func Command() *discordgo.ApplicationCommand {
	return &discordgo.ApplicationCommand{
		Name: "알림", Description: "영화 예매 오픈 알림을 관리합니다",
		Options: []*discordgo.ApplicationCommandOption{
			{Type: discordgo.ApplicationCommandOptionSubCommand, Name: "초기화", Description: "안내, 공지와 영화 예매 알림 채널을 준비합니다"},
			{Type: discordgo.ApplicationCommandOptionSubCommand, Name: "공지", Description: "공지 채널에 운영 안내를 게시합니다", Options: []*discordgo.ApplicationCommandOption{
				{Type: discordgo.ApplicationCommandOptionString, Name: "내용", Description: "게시할 공지 내용", Required: true, MaxLength: 1900},
			}},
		},
	}
}
