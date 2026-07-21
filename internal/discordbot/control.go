package discordbot

import (
	"bytes"
	"context"
	_ "embed"
	"errors"
	"fmt"
	"strings"
	"time"

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
	GuildChannels(string, ...discordgo.RequestOption) ([]*discordgo.Channel, error)
	GuildRoles(string, ...discordgo.RequestOption) ([]*discordgo.Role, error)
	GuildMember(string, string, ...discordgo.RequestOption) (*discordgo.Member, error)
	GuildRoleCreate(string, *discordgo.RoleParams, ...discordgo.RequestOption) (*discordgo.Role, error)
	GuildRoleDelete(string, string, ...discordgo.RequestOption) error
	GuildMemberRoleAdd(string, string, string, ...discordgo.RequestOption) error
	ChannelDelete(string, ...discordgo.RequestOption) (*discordgo.Channel, error)
	ChannelMessageDelete(string, string, ...discordgo.RequestOption) error
	ChannelMessages(string, int, string, string, string, ...discordgo.RequestOption) ([]*discordgo.Message, error)
}

//go:embed assets/my-movie-pepe.png
var guideImage []byte

type ChannelManager struct {
	session channelSession
	botID   func() string
}

func NewChannelManager(session channelSession, botID func() string) *ChannelManager {
	return &ChannelManager{session: session, botID: botID}
}

func (m *ChannelManager) EnsureViewerRole(ctx context.Context, guildID, existingID, name string) (string, bool, error) {
	roles, err := m.session.GuildRoles(guildID, discordgo.WithContext(ctx))
	if err != nil {
		return "", false, err
	}
	botMember, err := m.session.GuildMember(guildID, m.botID(), discordgo.WithContext(ctx))
	if err != nil {
		return "", false, err
	}
	botPosition, err := highestRolePosition(roles, botMember.Roles)
	if err != nil {
		return "", false, err
	}
	if existingID != "" {
		for _, role := range roles {
			if role.ID == existingID && role.ID != guildID && isSafeViewerRole(role, botPosition) {
				return role.ID, false, nil
			}
		}
	}
	if existingID != "" {
		for _, role := range roles {
			if role.ID != guildID && role.Name == name && isSafeViewerRole(role, botPosition) {
				return role.ID, false, nil
			}
		}
	}
	permissions := int64(0)
	mentionable := false
	role, err := m.session.GuildRoleCreate(guildID, &discordgo.RoleParams{Name: name, Permissions: &permissions, Mentionable: &mentionable}, discordgo.WithContext(ctx))
	if err != nil {
		return "", false, err
	}
	refreshedRoles, err := m.session.GuildRoles(guildID, discordgo.WithContext(ctx))
	if err != nil {
		return "", false, errors.Join(err, m.deleteRoleDetached(ctx, guildID, role.ID))
	}
	botPosition, err = highestRolePosition(refreshedRoles, botMember.Roles)
	if err != nil {
		return "", false, errors.Join(err, m.deleteRoleDetached(ctx, guildID, role.ID))
	}
	found := false
	for _, candidate := range refreshedRoles {
		if candidate.ID == role.ID {
			role = candidate
			found = true
			break
		}
	}
	if !found || !isSafeViewerRole(role, botPosition) {
		err := errors.New("created alert viewer role is not manageable by the bot")
		return "", false, errors.Join(err, m.deleteRoleDetached(ctx, guildID, role.ID))
	}
	return role.ID, true, nil
}

func (m *ChannelManager) deleteRoleDetached(ctx context.Context, guildID, roleID string) error {
	cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
	defer cancel()
	return m.session.GuildRoleDelete(guildID, roleID, discordgo.WithContext(cleanupCtx))
}

func highestRolePosition(roles []*discordgo.Role, roleIDs []string) (int, error) {
	ids := make(map[string]struct{}, len(roleIDs))
	for _, id := range roleIDs {
		ids[id] = struct{}{}
	}
	highest := -1
	for _, role := range roles {
		if _, ok := ids[role.ID]; ok && role.Position > highest {
			highest = role.Position
		}
	}
	if highest < 0 {
		return 0, errors.New("could not determine the bot role position")
	}
	return highest, nil
}

func isSafeViewerRole(role *discordgo.Role, botRolePosition int) bool {
	return role != nil && !role.Managed && role.Permissions == 0 && role.Position < botRolePosition
}

func (m *ChannelManager) EnsurePublicCategory(ctx context.Context, guildID, existingID, name string) (string, bool, error) {
	return m.ensureChannel(ctx, guildID, "", existingID, name, discordgo.ChannelTypeGuildCategory, publicOverwrites(guildID, m.botID()), existingID != "")
}

func (m *ChannelManager) EnsureRestrictedCategory(ctx context.Context, guildID, existingID, name, viewerRoleID string) (string, bool, error) {
	return m.ensureChannel(ctx, guildID, "", existingID, name, discordgo.ChannelTypeGuildCategory, restrictedOverwrites(guildID, viewerRoleID, m.botID()), existingID != "")
}

func (m *ChannelManager) ensureChannel(ctx context.Context, guildID, categoryID, existingID, name string, channelType discordgo.ChannelType, overwrites []*discordgo.PermissionOverwrite, recoverByName bool) (string, bool, error) {
	if existingID != "" {
		if channel, err := m.session.Channel(existingID, discordgo.WithContext(ctx)); err == nil && channel.GuildID == guildID && channel.Type == channelType {
			edited, err := m.session.ChannelEditComplex(existingID, &discordgo.ChannelEdit{Name: name, ParentID: categoryID, PermissionOverwrites: overwrites}, discordgo.WithContext(ctx))
			if err != nil {
				return "", false, err
			}
			return edited.ID, false, nil
		}
	}
	if recoverByName {
		channels, err := m.session.GuildChannels(guildID, discordgo.WithContext(ctx))
		if err != nil {
			return "", false, err
		}
		for _, channel := range channels {
			if channel.Type == channelType && channel.Name == name && (channelType == discordgo.ChannelTypeGuildCategory || channel.ParentID == categoryID) {
				edited, err := m.session.ChannelEditComplex(channel.ID, &discordgo.ChannelEdit{Name: name, ParentID: categoryID, PermissionOverwrites: overwrites}, discordgo.WithContext(ctx))
				if err != nil {
					return "", false, err
				}
				return edited.ID, false, nil
			}
		}
	}
	channel, err := m.session.GuildChannelCreateComplex(guildID, discordgo.GuildChannelCreateData{
		Name: name, Type: channelType, ParentID: categoryID, PermissionOverwrites: overwrites,
	}, discordgo.WithContext(ctx))
	if err != nil {
		return "", false, err
	}
	return channel.ID, true, nil
}

func (m *ChannelManager) EnsurePublicTextChannel(ctx context.Context, guildID, categoryID, existingID, name string) (string, bool, error) {
	return m.ensureChannel(ctx, guildID, categoryID, existingID, name, discordgo.ChannelTypeGuildText, publicOverwrites(guildID, m.botID()), existingID != "")
}

func (m *ChannelManager) EnsureRestrictedTextChannel(ctx context.Context, guildID, categoryID, existingID, name, viewerRoleID string) (string, bool, error) {
	return m.ensureChannel(ctx, guildID, categoryID, existingID, name, discordgo.ChannelTypeGuildText, restrictedOverwrites(guildID, viewerRoleID, m.botID()), true)
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

func (m *ChannelManager) EnsurePrivateTextChannel(ctx context.Context, guildID, categoryID, existingID, name, ownerID string) (string, bool, error) {
	return m.ensureChannel(ctx, guildID, categoryID, existingID, name, discordgo.ChannelTypeGuildText, privateOverwrites(guildID, ownerID, m.botID()), true)
}

func (m *ChannelManager) UpsertPanel(ctx context.Context, channelID, existingID string, panel control.Panel) (string, bool, error) {
	message := panelMessage(panel)
	if existingID != "" {
		components := message.Components
		embeds := []*discordgo.MessageEmbed{}
		if _, err := m.session.ChannelMessageEditComplex(&discordgo.MessageEdit{
			ID: existingID, Channel: channelID, Content: &message.Content,
			Components: &components, Embeds: &embeds,
		}, discordgo.WithContext(ctx)); err == nil {
			return existingID, false, nil
		} else if !isUnknownMessage(err) {
			return "", false, err
		}
	}
	created, err := m.session.ChannelMessageSendComplex(channelID, message, discordgo.WithContext(ctx))
	if err != nil {
		return "", false, err
	}
	return created.ID, true, nil
}

func (m *ChannelManager) UpsertGuide(ctx context.Context, channelID, existingID string) (string, bool, error) {
	message := guideMessage()
	if existingID == "" {
		messages, err := m.session.ChannelMessages(channelID, 100, "", "", "", discordgo.WithContext(ctx))
		if err != nil {
			return "", false, err
		}
		for _, candidate := range messages {
			if isGuideMessage(candidate, m.botID()) {
				existingID = candidate.ID
				break
			}
		}
	}
	if existingID != "" {
		components := message.Components
		embeds := []*discordgo.MessageEmbed{}
		attachments := []*discordgo.MessageAttachment{}
		if _, err := m.session.ChannelMessageEditComplex(&discordgo.MessageEdit{
			ID: existingID, Channel: channelID, Content: &message.Content, Components: &components,
			Embeds: &embeds, AllowedMentions: message.AllowedMentions, Files: message.Files, Attachments: &attachments,
		}, discordgo.WithContext(ctx)); err == nil {
			return existingID, false, nil
		} else if !isUnknownMessage(err) {
			return "", false, err
		}
		message = guideMessage()
	}
	created, err := m.session.ChannelMessageSendComplex(channelID, message, discordgo.WithContext(ctx))
	if err != nil {
		return "", false, err
	}
	return created.ID, true, nil
}

func isGuideMessage(message *discordgo.Message, botID string) bool {
	if message == nil || message.Author == nil || message.Author.ID != botID {
		return false
	}
	for _, component := range message.Components {
		row, ok := component.(*discordgo.ActionsRow)
		if !ok {
			if value, valueOK := component.(discordgo.ActionsRow); valueOK {
				row = &value
				ok = true
			}
		}
		if !ok {
			continue
		}
		for _, child := range row.Components {
			switch button := child.(type) {
			case discordgo.Button:
				if button.CustomID == "alerts:join" {
					return true
				}
			case *discordgo.Button:
				if button.CustomID == "alerts:join" {
					return true
				}
			}
		}
	}
	return false
}

func (m *ChannelManager) AddMemberRole(ctx context.Context, guildID, userID, roleID string) error {
	roles, err := m.session.GuildRoles(guildID, discordgo.WithContext(ctx))
	if err != nil {
		return err
	}
	botMember, err := m.session.GuildMember(guildID, m.botID(), discordgo.WithContext(ctx))
	if err != nil {
		return err
	}
	botPosition, err := highestRolePosition(roles, botMember.Roles)
	if err != nil {
		return err
	}
	var viewerRole *discordgo.Role
	for _, role := range roles {
		if role.ID == roleID {
			viewerRole = role
			break
		}
	}
	if !isSafeViewerRole(viewerRole, botPosition) {
		return errors.New("alert viewer role is missing, privileged, managed, or above the bot")
	}
	return m.session.GuildMemberRoleAdd(guildID, userID, roleID, discordgo.WithContext(ctx))
}

func (m *ChannelManager) DeleteRole(ctx context.Context, guildID, roleID string) error {
	return m.session.GuildRoleDelete(guildID, roleID, discordgo.WithContext(ctx))
}

func (m *ChannelManager) DeleteChannel(ctx context.Context, channelID string) error {
	_, err := m.session.ChannelDelete(channelID, discordgo.WithContext(ctx))
	return err
}

func (m *ChannelManager) DeleteMessage(ctx context.Context, channelID, messageID string) error {
	return m.session.ChannelMessageDelete(channelID, messageID, discordgo.WithContext(ctx))
}

func (m *ChannelManager) SendAnnouncement(ctx context.Context, channelID, content string) error {
	_, err := m.session.ChannelMessageSendComplex(channelID, &discordgo.MessageSend{
		Content:         content,
		AllowedMentions: &discordgo.MessageAllowedMentions{Parse: []discordgo.AllowedMentionType{}},
	}, discordgo.WithContext(ctx))
	return err
}

func isUnknownMessage(err error) bool {
	var restError *discordgo.RESTError
	return errors.As(err, &restError) && restError.Message != nil && restError.Message.Code == 10008
}

func privateOverwrites(guildID, ownerID, botID string) []*discordgo.PermissionOverwrite {
	ownerAllow := int64(discordgo.PermissionViewChannel | discordgo.PermissionReadMessageHistory)
	botAllow := ownerAllow | discordgo.PermissionSendMessages | discordgo.PermissionManageChannels
	memberDeny := int64(discordgo.PermissionViewChannel | discordgo.PermissionSendMessages | discordgo.PermissionCreatePublicThreads | discordgo.PermissionCreatePrivateThreads | discordgo.PermissionSendMessagesInThreads)
	return []*discordgo.PermissionOverwrite{
		{ID: guildID, Type: discordgo.PermissionOverwriteTypeRole, Deny: memberDeny},
		{ID: ownerID, Type: discordgo.PermissionOverwriteTypeMember, Allow: ownerAllow, Deny: int64(discordgo.PermissionSendMessages | discordgo.PermissionCreatePublicThreads | discordgo.PermissionCreatePrivateThreads | discordgo.PermissionSendMessagesInThreads)},
		{ID: botID, Type: discordgo.PermissionOverwriteTypeMember, Allow: botAllow},
	}
}

func restrictedOverwrites(guildID, viewerRoleID, botID string) []*discordgo.PermissionOverwrite {
	viewerAllow := int64(discordgo.PermissionViewChannel | discordgo.PermissionReadMessageHistory | discordgo.PermissionAddReactions)
	memberDeny := int64(discordgo.PermissionSendMessages | discordgo.PermissionCreatePublicThreads | discordgo.PermissionCreatePrivateThreads | discordgo.PermissionSendMessagesInThreads)
	botAllow := int64(discordgo.PermissionViewChannel | discordgo.PermissionReadMessageHistory | discordgo.PermissionSendMessages | discordgo.PermissionAddReactions | discordgo.PermissionManageChannels | discordgo.PermissionCreatePublicThreads | discordgo.PermissionCreatePrivateThreads | discordgo.PermissionSendMessagesInThreads)
	return []*discordgo.PermissionOverwrite{
		{ID: guildID, Type: discordgo.PermissionOverwriteTypeRole, Deny: memberDeny | discordgo.PermissionViewChannel},
		{ID: viewerRoleID, Type: discordgo.PermissionOverwriteTypeRole, Allow: viewerAllow, Deny: memberDeny},
		{ID: botID, Type: discordgo.PermissionOverwriteTypeMember, Allow: botAllow},
	}
}

func publicOverwrites(guildID, botID string) []*discordgo.PermissionOverwrite {
	memberAllow := int64(discordgo.PermissionViewChannel | discordgo.PermissionReadMessageHistory | discordgo.PermissionAddReactions)
	memberDeny := int64(discordgo.PermissionSendMessages | discordgo.PermissionCreatePublicThreads | discordgo.PermissionCreatePrivateThreads | discordgo.PermissionSendMessagesInThreads)
	botAllow := int64(discordgo.PermissionViewChannel | discordgo.PermissionReadMessageHistory | discordgo.PermissionSendMessages | discordgo.PermissionAddReactions | discordgo.PermissionManageChannels | discordgo.PermissionCreatePublicThreads | discordgo.PermissionCreatePrivateThreads | discordgo.PermissionSendMessagesInThreads)
	return []*discordgo.PermissionOverwrite{
		{ID: guildID, Type: discordgo.PermissionOverwriteTypeRole, Allow: memberAllow, Deny: memberDeny},
		{ID: botID, Type: discordgo.PermissionOverwriteTypeMember, Allow: botAllow},
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

func guideMessage() *discordgo.MessageSend {
	return &discordgo.MessageSend{
		Content: `🎬 **영화 예매 오픈 알림**

CGV와 메가박스의 특별관 예매 일정이 새로 열리면 Discord로 알려드립니다.

**지원 영화관**

메가박스
• 코엑스 · Dolby Cinema
• 남양주현대아울렛 스페이스원 · Dolby Cinema

CGV
• 용산아이파크몰 · IMAX
• 용산아이파크몰 · 4DX
• 용산아이파크몰 · SCREENX

**채널 안내**

• ` + "`공지`" + `: 기능 업데이트와 운영 안내
• ` + "`서버-상태`" + `: 영화관 조회 장애와 복구 상태
• 영화관별 채널: 해당 특별관의 새 예매 회차 알림

**이용 방법**

아래 ` + "`알림 채널 보기`" + ` 버튼을 누르면 영화관별 알림 채널이 표시됩니다.
예매 알림의 앱 열기 버튼을 누르면 해당 영화관 앱이 실행됩니다.

예매 가능 여부와 잔여 좌석은 영화관 앱에서 최종 확인해 주세요.`,
		Components: []discordgo.MessageComponent{discordgo.ActionsRow{Components: []discordgo.MessageComponent{
			discordgo.Button{Label: "🔔 알림 채널 보기", Style: discordgo.PrimaryButton, CustomID: "alerts:join"},
		}}},
		Files:           []*discordgo.File{{Name: "my-movie-pepe.png", ContentType: "image/png", Reader: bytes.NewReader(guideImage)}},
		AllowedMentions: &discordgo.MessageAllowedMentions{Parse: []discordgo.AllowedMentionType{}},
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
