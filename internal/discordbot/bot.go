package discordbot

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/bwmarrin/discordgo"

	"my-movie/internal/database"
)

type Controller interface {
	Initialize(context.Context, string, string) (database.Installation, error)
	SelectTarget(context.Context, string, string) error
	Enable(context.Context, string, string) error
	Disable(context.Context, string, string) error
	JoinAlerts(context.Context, string) error
	Announce(context.Context, string, string) error
}

type Bot struct {
	session       *discordgo.Session
	guildID       string
	controller    Controller
	removeHandler func()
	joinMu        sync.Mutex
	joinAttempts  map[string]time.Time
}

func NewBot(session *discordgo.Session, guildID string, controller Controller) *Bot {
	return &Bot{session: session, guildID: guildID, controller: controller, joinAttempts: make(map[string]time.Time)}
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
	if len(data.Options) != 1 {
		b.respond(session, interaction, "알 수 없는 하위 명령입니다.")
		return
	}
	b.deferResponse(session, interaction)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	content := b.executeCommand(ctx, interaction.GuildID, interactionUserID(interaction), data)
	_, _ = session.InteractionResponseEdit(interaction.Interaction, &discordgo.WebhookEdit{Content: &content})
}

func (b *Bot) executeCommand(ctx context.Context, guildID, userID string, data discordgo.ApplicationCommandInteractionData) string {
	if len(data.Options) != 1 {
		return "알 수 없는 하위 명령입니다."
	}
	switch option := data.Options[0]; option.Name {
	case "초기화":
		if _, err := b.controller.Initialize(ctx, guildID, userID); err != nil {
			return "초기화하지 못했습니다: " + err.Error()
		}
		return "안내, 공지와 영화 예매 알림 채널을 준비했습니다."
	case "공지":
		if len(option.Options) != 1 || option.Options[0].Name != "내용" {
			return "공지 내용을 확인하지 못했습니다."
		}
		if err := b.controller.Announce(ctx, userID, option.Options[0].StringValue()); err != nil {
			return "공지를 게시하지 못했습니다: " + err.Error()
		}
		return "공지를 게시했습니다."
	default:
		return "알 수 없는 하위 명령입니다."
	}
}

func (b *Bot) handleComponent(session *discordgo.Session, interaction *discordgo.InteractionCreate) {
	data := interaction.MessageComponentData()
	userID := interactionUserID(interaction)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	var err error
	if data.CustomID == "alerts:join" {
		b.deferResponse(session, interaction)
		if !b.allowJoin(userID) {
			content := "알림 채널 보기는 10초에 한 번만 사용할 수 있습니다. 잠시 후 다시 시도해 주세요."
			_, _ = session.InteractionResponseEdit(interaction.Interaction, &discordgo.WebhookEdit{Content: &content})
			return
		}
		content := b.executeJoin(ctx, userID)
		_, _ = session.InteractionResponseEdit(interaction.Interaction, &discordgo.WebhookEdit{Content: &content})
		return
	} else if data.CustomID == "alerts:target" {
		if len(data.Values) != 1 {
			return
		}
		_ = session.InteractionRespond(interaction.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseDeferredMessageUpdate,
		})
		err = b.controller.SelectTarget(ctx, userID, data.Values[0])
		if err != nil {
			_, _ = session.FollowupMessageCreate(interaction.Interaction, true, &discordgo.WebhookParams{
				Content: "대상을 선택하지 못했습니다: " + err.Error(),
				Flags:   discordgo.MessageFlagsEphemeral,
			})
		}
		return
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
}

func (b *Bot) allowJoin(userID string) bool {
	now := time.Now()
	cutoff := now.Add(-10 * time.Second)
	b.joinMu.Lock()
	defer b.joinMu.Unlock()
	for id, attemptedAt := range b.joinAttempts {
		if attemptedAt.Before(cutoff) {
			delete(b.joinAttempts, id)
		}
	}
	if attemptedAt, exists := b.joinAttempts[userID]; exists && now.Sub(attemptedAt) < 10*time.Second {
		return false
	}
	b.joinAttempts[userID] = now
	return true
}

func (b *Bot) executeJoin(ctx context.Context, userID string) string {
	if err := b.controller.JoinAlerts(ctx, userID); err != nil {
		slog.Error("assign alert viewer role failed", "user_id", userID, "error", err)
		return "알림 채널을 열지 못했습니다. 잠시 후 다시 시도해 주세요."
	}
	return "알림 채널을 열었습니다."
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
