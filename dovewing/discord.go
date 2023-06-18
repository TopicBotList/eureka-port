package dovewing

import (
	"context"
	"errors"
	"strconv"

	"github.com/bwmarrin/discordgo"
)

func discordPlatformStatus(status discordgo.Status) PlatformStatus {
	switch status {
	case discordgo.StatusOnline:
		return PlatformStatusOnline
	case discordgo.StatusIdle:
		return PlatformStatusIdle
	case discordgo.StatusDoNotDisturb:
		return PlatformStatusDoNotDisturb
	default:
		return PlatformStatusOffline
	}
}

type DiscordState struct {
	Session        *discordgo.Session // Discord session
	PreferredGuild string             // Which guilds should be checked first for users, good if theres one guild with the majority of users
	Initted        bool               // Whether the platform has been initted or not
}

func (d *DiscordState) platformName() string {
	return "discord"
}

func (d *DiscordState) init() error {
	if d.Session == nil {
		return errors.New("discord not enabled")
	}

	d.Initted = true
	return nil
}

func (d *DiscordState) initted() bool {
	return d.Initted
}

func (d *DiscordState) validateId(id string) (string, error) {
	// Before wasting time searching state, ensure the ID is actually a valid snowflake
	if _, err := strconv.ParseUint(id, 10, 64); err != nil {
		return "", err
	}

	// For all practical purposes, a simple length check can handle a lot of illegal IDs
	if len(id) <= 16 || len(id) > 20 {
		return "", errors.New("invalid snowflake")
	}

	return id, nil
}

func (d *DiscordState) platformSpecificCache(ctx context.Context, id string) (*PlatformUser, error) {
	// First try for main server
	if d.PreferredGuild != "" {
		member, err := d.Session.State.Member(d.PreferredGuild, id)

		if err == nil {
			p, pErr := d.Session.State.Presence(d.PreferredGuild, id)

			if pErr != nil {
				p = &discordgo.Presence{
					User:   member.User,
					Status: discordgo.StatusOffline,
				}
			}

			return &PlatformUser{
				ID:          id,
				Username:    member.User.Username,
				Avatar:      member.User.AvatarURL(""),
				DisplayName: member.User.GlobalName,
				Bot:         member.User.Bot,
				ExtraData: map[string]any{
					"nickname":        member.Nick,
					"mutual_guild":    d.PreferredGuild,
					"preferred_guild": true,
					"public_flags":    member.User.PublicFlags,
				},
				Status: discordPlatformStatus(p.Status),
			}, nil
		}
	}

	for _, guild := range d.Session.State.Guilds {
		if guild.ID == d.PreferredGuild {
			continue // Already checked
		}

		member, err := d.Session.State.Member(guild.ID, id)

		if err == nil {
			p, pErr := d.Session.State.Presence(guild.ID, id)

			if pErr != nil {
				p = &discordgo.Presence{
					User:   member.User,
					Status: discordgo.StatusOffline,
				}
			}

			return &PlatformUser{
				ID:          id,
				Username:    member.User.Username,
				Avatar:      member.User.AvatarURL(""),
				DisplayName: member.User.GlobalName,
				Bot:         member.User.Bot,
				ExtraData: map[string]any{
					"nickname":        member.Nick,
					"mutual_guild":    guild.ID,
					"preferred_guild": false,
					"public_flags":    member.User.PublicFlags,
				},
				Status: discordPlatformStatus(p.Status),
			}, nil
		}
	}

	return nil, nil
}

func (d *DiscordState) getUser(ctx context.Context, id string) (*PlatformUser, error) {
	// Get from discord
	user, err := d.Session.User(id)

	if err != nil {
		return nil, err
	}

	return &PlatformUser{
		ID:          id,
		Username:    user.Username,
		Avatar:      user.AvatarURL(""),
		DisplayName: user.GlobalName,
		Bot:         user.Bot,
		Status:      PlatformStatusOffline,
	}, nil
}
