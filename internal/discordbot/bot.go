package discordbot

import (
	"context"
	"fmt"
	"strings"

	"github.com/bwmarrin/discordgo"

	"my-movie/internal/domain"
	"my-movie/internal/providers"
)

type Bot struct {
	session       *discordgo.Session
	guildID       string
	handler       *Handler
	removeHandler func()
}

func NewBot(session *discordgo.Session, guildID string, handler *Handler) *Bot {
	return &Bot{session: session, guildID: guildID, handler: handler}
}

func (b *Bot) Start() error {
	b.removeHandler = b.session.AddHandler(b.handleInteraction)
	if err := b.session.Open(); err != nil {
		b.removeHandler()
		b.removeHandler = nil
		return err
	}
	return nil
}

func (b *Bot) Stop() error {
	if b.removeHandler != nil {
		b.removeHandler()
		b.removeHandler = nil
	}
	return b.session.Close()
}

func (b *Bot) handleInteraction(session *discordgo.Session, interaction *discordgo.InteractionCreate) {
	if interaction.GuildID != b.guildID || interaction.ApplicationCommandData().Name != "알림" {
		return
	}
	if interaction.Type == discordgo.InteractionApplicationCommandAutocomplete {
		b.handleAutocomplete(session, interaction)
		return
	}
	b.handleCommand(session, interaction)
}

func (b *Bot) handleAutocomplete(session *discordgo.Session, interaction *discordgo.InteractionCreate) {
	data := interaction.ApplicationCommandData()
	if len(data.Options) == 0 {
		return
	}
	subcommand := data.Options[0]
	values := optionValues(subcommand.Options)
	focused, query := focusedOption(subcommand.Options)
	var (
		choices []Choice
		err     error
	)
	switch {
	case subcommand.Name == "등록" && focused == "지점":
		choices, err = b.handler.TheaterChoices(context.Background(), domain.ProviderID(values["영화관"]), query)
	case subcommand.Name == "등록" && focused == "영화":
		choices, err = b.handler.MovieChoices(context.Background(), domain.ProviderID(values["영화관"]), query)
	case subcommand.Name == "삭제" && focused == "알림":
		choices, err = b.handler.DeleteChoices(context.Background(), interactionUserID(interaction), query)
	}
	if err != nil {
		choices = nil
	}
	_ = session.InteractionRespond(interaction.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionApplicationCommandAutocompleteResult,
		Data: &discordgo.InteractionResponseData{Choices: discordChoices(choices)},
	})
}

func (b *Bot) handleCommand(session *discordgo.Session, interaction *discordgo.InteractionCreate) {
	data := interaction.ApplicationCommandData()
	if len(data.Options) == 0 {
		b.respond(session, interaction, "하위 명령을 선택해주세요.")
		return
	}
	subcommand := data.Options[0]
	values := optionValues(subcommand.Options)
	userID := interactionUserID(interaction)
	switch subcommand.Name {
	case "등록":
		_ = session.InteractionRespond(interaction.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseDeferredChannelMessageWithSource,
			Data: &discordgo.InteractionResponseData{Flags: discordgo.MessageFlagsEphemeral},
		})
		item, err := b.handler.Register(context.Background(), userID, domain.ProviderID(values["영화관"]), values["지점"], values["영화"])
		content := fmt.Sprintf("등록했습니다: %s · %s · %s", providerName(item.Provider), item.Theater.Name, item.Movie.Name)
		if err != nil {
			content = "등록하지 못했습니다: " + err.Error()
		}
		_, _ = session.InteractionResponseEdit(interaction.Interaction, &discordgo.WebhookEdit{Content: &content})
	case "목록":
		items, err := b.handler.List(context.Background(), userID)
		if err != nil {
			b.respond(session, interaction, "목록을 불러오지 못했습니다: "+err.Error())
			return
		}
		if len(items) == 0 {
			b.respond(session, interaction, "등록된 알림이 없습니다.")
			return
		}
		lines := make([]string, 0, len(items))
		for _, item := range items {
			lines = append(lines, fmt.Sprintf("• %s · %s · %s", providerName(item.Provider), item.Theater.Name, item.Movie.Name))
		}
		b.respond(session, interaction, strings.Join(lines, "\n"))
	case "삭제":
		if err := b.handler.Delete(context.Background(), userID, values["알림"]); err != nil {
			b.respond(session, interaction, "삭제하지 못했습니다: "+err.Error())
			return
		}
		b.respond(session, interaction, "알림을 삭제했습니다.")
	case "전체삭제":
		count, err := b.handler.DeleteAll(context.Background(), userID)
		if err != nil {
			b.respond(session, interaction, "삭제하지 못했습니다: "+err.Error())
			return
		}
		b.respond(session, interaction, fmt.Sprintf("알림 %d개를 삭제했습니다.", count))
	case "도움말":
		b.respond(session, interaction, "`/알림 등록`으로 영화관·지점·영화를 고르면 새 예매 회차를 DM으로 알려드립니다. `/알림 목록`, `/알림 삭제`, `/알림 전체삭제`로 관리할 수 있습니다.")
	default:
		b.respond(session, interaction, "알 수 없는 하위 명령입니다.")
	}
}

func (b *Bot) respond(session *discordgo.Session, interaction *discordgo.InteractionCreate, content string) {
	_ = session.InteractionRespond(interaction.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{Content: content, Flags: discordgo.MessageFlagsEphemeral},
	})
}

func interactionUserID(interaction *discordgo.InteractionCreate) string {
	if interaction.Member != nil && interaction.Member.User != nil {
		return interaction.Member.User.ID
	}
	if interaction.User != nil {
		return interaction.User.ID
	}
	return ""
}

func NewSession(token string) (*discordgo.Session, error) {
	session, err := discordgo.New("Bot " + token)
	if err != nil {
		return nil, err
	}
	session.Identify.Intents = discordgo.IntentsGuilds
	return session, nil
}

func EnabledCommand(registry *providers.Registry) *discordgo.ApplicationCommand {
	return Command(registry)
}
