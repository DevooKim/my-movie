package main

import (
	"fmt"
	"log"

	"github.com/bwmarrin/discordgo"

	"my-movie/internal/config"
	"my-movie/internal/discordbot"
)

func main() {
	configuration, err := config.Load()
	if err != nil {
		log.Fatal(err)
	}
	session, err := discordbot.NewSession(configuration.DiscordBotToken)
	if err != nil {
		log.Fatal(err)
	}
	commands, err := session.ApplicationCommandBulkOverwrite(
		configuration.DiscordApplicationID,
		configuration.DiscordGuildID,
		[]*discordgo.ApplicationCommand{discordbot.EnabledCommand()},
	)
	if err != nil {
		log.Fatal(err)
	}
	for _, command := range commands {
		fmt.Println(command.Name)
	}
}
