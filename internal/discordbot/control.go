package discordbot

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/bwmarrin/discordgo"

	"my-movie/internal/control"
	"my-movie/internal/targets"
)

type channelSession interface {
	Channel(string, ...discordgo.RequestOption) (*discordgo.Channel, error)
	ChannelEditComplex(string, *discordgo.ChannelEdit, ...discordgo.RequestOption) (*discordgo.Channel, error)
	GuildChannelCreateComplex(string, discordgo.GuildChannelCreateData, ...discordgo.RequestOption) (*discordgo.Channel, error)
	ChannelMessageSendComplex(string, *discordgo.MessageSend, ...discordgo.RequestOption) (*discordgo.Message, error)
	ChannelMessageEditComplex(*discordgo.MessageEdit, ...discordgo.RequestOption) (*discordgo.Message, error)
}

type ChannelManager struct {
	session channelSession
	botID   func() string
}

func NewChannelManager(session channelSession, botID func() string) *ChannelManager {
	return &ChannelManager{session: session, botID: botID}
}

func (m *ChannelManager) EnsurePrivateCategory(ctx context.Context, guildID, existingID, name, ownerID string) (string, error) {
	if existingID != "" {
		if channel, err := m.session.Channel(existingID, discordgo.WithContext(ctx)); err == nil && channel.GuildID == guildID && channel.Type == discordgo.ChannelTypeGuildCategory {
			edited, err := m.session.ChannelEditComplex(existingID, &discordgo.ChannelEdit{Name: name, PermissionOverwrites: privateOverwrites(guildID, ownerID, m.botID())}, discordgo.WithContext(ctx))
			if err != nil {
				return "", err
			}
			return edited.ID, nil
		}
	}
	channel, err := m.session.GuildChannelCreateComplex(guildID, discordgo.GuildChannelCreateData{
		Name: name, Type: discordgo.ChannelTypeGuildCategory,
		PermissionOverwrites: privateOverwrites(guildID, ownerID, m.botID()),
	}, discordgo.WithContext(ctx))
	if err != nil {
		return "", err
	}
	return channel.ID, nil
}

func (m *ChannelManager) EnsurePrivateTextChannel(ctx context.Context, guildID, categoryID, existingID, name, ownerID string) (string, error) {
	if existingID != "" {
		if channel, err := m.session.Channel(existingID, discordgo.WithContext(ctx)); err == nil && channel.GuildID == guildID && channel.Type == discordgo.ChannelTypeGuildText {
			edited, err := m.session.ChannelEditComplex(existingID, &discordgo.ChannelEdit{Name: name, ParentID: categoryID, PermissionOverwrites: privateOverwrites(guildID, ownerID, m.botID())}, discordgo.WithContext(ctx))
			if err != nil {
				return "", err
			}
			return edited.ID, nil
		}
	}
	channel, err := m.session.GuildChannelCreateComplex(guildID, discordgo.GuildChannelCreateData{
		Name: name, Type: discordgo.ChannelTypeGuildText, ParentID: categoryID,
		PermissionOverwrites: privateOverwrites(guildID, ownerID, m.botID()),
	}, discordgo.WithContext(ctx))
	if err != nil {
		return "", err
	}
	return channel.ID, nil
}

func (m *ChannelManager) UpsertPanel(ctx context.Context, channelID, existingID string, panel control.Panel) (string, error) {
	message := panelMessage(panel)
	if existingID != "" {
		components := message.Components
		embeds := []*discordgo.MessageEmbed{}
		if _, err := m.session.ChannelMessageEditComplex(&discordgo.MessageEdit{
			ID: existingID, Channel: channelID, Content: &message.Content,
			Components: &components, Embeds: &embeds,
		}, discordgo.WithContext(ctx)); err == nil {
			return existingID, nil
		} else if !isUnknownMessage(err) {
			return "", err
		}
	}
	created, err := m.session.ChannelMessageSendComplex(channelID, message, discordgo.WithContext(ctx))
	if err != nil {
		return "", err
	}
	return created.ID, nil
}

func isUnknownMessage(err error) bool {
	var restError *discordgo.RESTError
	return errors.As(err, &restError) && restError.Message != nil && restError.Message.Code == 10008
}

func privateOverwrites(guildID, ownerID, botID string) []*discordgo.PermissionOverwrite {
	memberAllow := int64(discordgo.PermissionViewChannel | discordgo.PermissionSendMessages | discordgo.PermissionReadMessageHistory)
	return []*discordgo.PermissionOverwrite{
		{ID: guildID, Type: discordgo.PermissionOverwriteTypeRole, Deny: discordgo.PermissionViewChannel},
		{ID: ownerID, Type: discordgo.PermissionOverwriteTypeMember, Allow: memberAllow},
		{ID: botID, Type: discordgo.PermissionOverwriteTypeMember, Allow: memberAllow | discordgo.PermissionManageChannels},
	}
}

func panelMessage(panel control.Panel) *discordgo.MessageSend {
	var content strings.Builder
	content.WriteString("영화관별 예매 오픈 알림\n\n")
	for _, target := range panel.Targets {
		status := "OFF"
		if target.Enabled {
			status = "ON"
		}
		fmt.Fprintf(&content, "• %s: %s\n", target.Name, status)
	}
	options := make([]discordgo.SelectMenuOption, 0, len(targets.All()))
	for _, target := range targets.All() {
		options = append(options, discordgo.SelectMenuOption{
			Label: target.DisplayName(), Value: target.ID,
			Default: target.ID == panel.SelectedTargetID,
		})
	}
	disabled := panel.SelectedTargetID == ""
	return &discordgo.MessageSend{
		Content: content.String(),
		Components: []discordgo.MessageComponent{
			discordgo.ActionsRow{Components: []discordgo.MessageComponent{
				discordgo.SelectMenu{CustomID: "alerts:target", Placeholder: "영화관 · 상영관 선택", Options: options},
			}},
			discordgo.ActionsRow{Components: []discordgo.MessageComponent{
				discordgo.Button{Label: "알림 켜기", Style: discordgo.SuccessButton, CustomID: "alerts:enable:" + panel.SelectedTargetID, Disabled: disabled},
				discordgo.Button{Label: "알림 끄기", Style: discordgo.DangerButton, CustomID: "alerts:disable:" + panel.SelectedTargetID, Disabled: disabled},
			}},
		},
	}
}

func parseComponentAction(raw string) (string, string, bool) {
	parts := strings.Split(raw, ":")
	if len(parts) != 3 || parts[0] != "alerts" || parts[2] == "" {
		return "", "", false
	}
	if parts[1] != "enable" && parts[1] != "disable" {
		return "", "", false
	}
	return parts[1], parts[2], true
}
