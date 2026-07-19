package discordbot

import (
	"context"
	"fmt"
	"time"

	"github.com/bwmarrin/discordgo"

	"my-movie/internal/database"
)

type Controller interface {
	Initialize(context.Context, string, string) (database.Installation, error)
	SelectTarget(context.Context, string, string) error
	Enable(context.Context, string, string) error
	Disable(context.Context, string, string) error
}

type Bot struct {
	session       *discordgo.Session
	guildID       string
	controller    Controller
	removeHandler func()
}

func NewBot(session *discordgo.Session, guildID string, controller Controller) *Bot {
	return &Bot{session: session, guildID: guildID, controller: controller}
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
	b.StopAccepting()
	return b.session.Close()
}

func (b *Bot) StopAccepting() error {
	if b.removeHandler != nil {
		b.removeHandler()
		b.removeHandler = nil
	}
	return nil
}

func (b *Bot) handleInteraction(session *discordgo.Session, interaction *discordgo.InteractionCreate) {
	if interaction.GuildID != b.guildID {
		return
	}
	switch interaction.Type {
	case discordgo.InteractionApplicationCommand:
		if interaction.ApplicationCommandData().Name == "알림" {
			b.handleCommand(session, interaction)
		}
	case discordgo.InteractionMessageComponent:
		b.handleComponent(session, interaction)
	}
}

func (b *Bot) handleCommand(session *discordgo.Session, interaction *discordgo.InteractionCreate) {
	data := interaction.ApplicationCommandData()
	if len(data.Options) != 1 || data.Options[0].Name != "초기화" {
		b.respond(session, interaction, "알 수 없는 하위 명령입니다.")
		return
	}
	b.deferResponse(session, interaction)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	_, err := b.controller.Initialize(ctx, interaction.GuildID, interactionUserID(interaction))
	content := "비공개 알림 채널과 제어 패널을 준비했습니다."
	if err != nil {
		content = "초기화하지 못했습니다: " + err.Error()
	}
	_, _ = session.InteractionResponseEdit(interaction.Interaction, &discordgo.WebhookEdit{Content: &content})
}

func (b *Bot) handleComponent(session *discordgo.Session, interaction *discordgo.InteractionCreate) {
	data := interaction.MessageComponentData()
	userID := interactionUserID(interaction)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	var err error
	if data.CustomID == "alerts:target" {
		if len(data.Values) != 1 {
			return
		}
		b.deferResponse(session, interaction)
		err = b.controller.SelectTarget(ctx, userID, data.Values[0])
	} else if action, targetID, ok := parseComponentAction(data.CustomID); ok {
		b.deferResponse(session, interaction)
		if action == "enable" {
			err = b.controller.Enable(ctx, userID, targetID)
		} else {
			err = b.controller.Disable(ctx, userID, targetID)
		}
		content := "설정을 반영했습니다."
		if err != nil {
			content = "설정을 변경하지 못했습니다: " + err.Error()
		}
		_, _ = session.InteractionResponseEdit(interaction.Interaction, &discordgo.WebhookEdit{Content: &content})
		return
	} else {
		return
	}
	content := "대상을 선택했습니다."
	if err != nil {
		content = "대상을 선택하지 못했습니다: " + err.Error()
	}
	_, _ = session.InteractionResponseEdit(interaction.Interaction, &discordgo.WebhookEdit{Content: &content})
}

func (b *Bot) deferResponse(session *discordgo.Session, interaction *discordgo.InteractionCreate) {
	_ = session.InteractionRespond(interaction.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseDeferredChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{Flags: discordgo.MessageFlagsEphemeral},
	})
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
		return nil, fmt.Errorf("create session: %w", err)
	}
	session.Identify.Intents = discordgo.IntentsGuilds
	return session, nil
}

func EnabledCommand() *discordgo.ApplicationCommand { return Command() }
