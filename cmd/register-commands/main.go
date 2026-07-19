package main

import (
	"fmt"
	"log"

	"github.com/bwmarrin/discordgo"

	"my-movie/internal/config"
	"my-movie/internal/discordbot"
	"my-movie/internal/httpx"
	"my-movie/internal/providers"
	"my-movie/internal/providers/megabox"
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
	registry := providers.New(megabox.New(httpx.NewClient(httpx.Options{}), nil))
	commands, err := session.ApplicationCommandBulkOverwrite(
		configuration.DiscordApplicationID,
		configuration.DiscordGuildID,
		[]*discordgo.ApplicationCommand{discordbot.EnabledCommand(registry)},
	)
	if err != nil {
		log.Fatal(err)
	}
	for _, command := range commands {
		fmt.Println(command.Name)
	}
}
