package dovewing

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/bwmarrin/discordgo"
	"go.uber.org/zap"
)

type DiscordState struct {
	Session     *discordgo.Session
	UpdateCache func(u *PlatformUser) error
}

type DiscordPlatformSpecific struct {
	Nickname string `json:"nickname" description:"The users nickname if in a mutual server"`
	Guild    string `json:"in_guild" description:"The guild (ID) the user is in if in a mutual server"`
}

func GetDiscordUser(ctx context.Context, id string) (userObj *PlatformUser, err error) {
	if state.Discord == nil {
		return nil, errors.New("discord not enabled")
	}

	const userExpiryTime = 8 * time.Hour

	cachedReturn := func(u *PlatformUser) (*PlatformUser, error) {
		if u == nil {
			return nil, errors.New("user not found")
		}

		if u.DisplayName == "" {
			u.DisplayName = u.Username
		}

		// Update internal_user_cache__discord
		_, err := state.Pool.Exec(state.Context, "INSERT INTO internal_user_cache__discord (id, username, display_name, avatar, bot) VALUES ($1, $2, $3, $4, $5) ON CONFLICT (id) DO UPDATE SET username = $2, display_name = $3, avatar = $4, bot = $5, last_updated = NOW()", u.ID, u.Username, u.DisplayName, u.Avatar, u.Bot)

		if err != nil {
			return nil, fmt.Errorf("failed to update internal user cache: %s", err)
		}

		if u.Bot && state.Discord.UpdateCache != nil {
			err := state.Discord.UpdateCache(u)
			if err != nil {
				return nil, fmt.Errorf("updateCache failed: %s", err)
			}
		}

		bytes, err := json.Marshal(u)

		if err == nil {
			state.Redis.Set(state.Context, "uobj__discord:"+id, bytes, userExpiryTime)
		}

		return u, nil
	}

	platformStatus := func(status discordgo.Status) PlatformStatus {
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

	// Before wasting time searching state, ensure the ID is actually a valid snowflake
	if _, err := strconv.ParseUint(id, 10, 64); err != nil {
		return nil, err
	}

	// For all practical purposes, a simple length check can handle a lot of illegal IDs
	if len(id) <= 16 || len(id) > 20 {
		return nil, errors.New("invalid snowflake")
	}

	// First try for main server
	member, err := state.Discord.Session.State.Member(state.PreferredGuild, id)

	if err == nil {
		p, pErr := state.Discord.Session.State.Presence(state.PreferredGuild, id)

		if pErr != nil {
			p = &discordgo.Presence{
				User:   member.User,
				Status: discordgo.StatusOffline,
			}
		}

		return cachedReturn(&PlatformUser{
			ID:          id,
			Username:    member.User.Username,
			Avatar:      member.User.AvatarURL(""),
			DisplayName: member.User.GlobalName,
			Bot:         member.User.Bot,
			ExtraData: map[string]any{
				"nickname":        member.Nick,
				"mutual_guild":    state.PreferredGuild,
				"preferred_guild": true,
			},
			Status: platformStatus(p.Status),
		})
	}

	for _, guild := range state.Discord.Session.State.Guilds {
		if guild.ID == state.PreferredGuild {
			continue // Already checked
		}

		member, err := state.Discord.Session.State.Member(guild.ID, id)

		if err == nil {
			p, pErr := state.Discord.Session.State.Presence(guild.ID, id)

			if pErr != nil {
				p = &discordgo.Presence{
					User:   member.User,
					Status: discordgo.StatusOffline,
				}
			}

			return cachedReturn(&PlatformUser{
				ID:          id,
				Username:    member.User.Username,
				Avatar:      member.User.AvatarURL(""),
				DisplayName: member.User.GlobalName,
				Bot:         member.User.Bot,
				ExtraData: map[string]any{
					"nickname":        member.Nick,
					"mutual_guild":    guild.ID,
					"preferred_guild": false,
				},
				Status: platformStatus(p.Status),
			})
		}
	}

	// Check if in redis cache
	userBytes, err := state.Redis.Get(ctx, "uobj__discord:"+id).Result()

	if err == nil {
		// Try to unmarshal

		var user PlatformUser

		err = json.Unmarshal([]byte(userBytes), &user)

		if err == nil {
			return &user, nil
		}
	}

	// Check if in internal_user_cache__discord, this allows fetches of users not in cache to be done in the background
	var count int64

	err = state.Pool.QueryRow(ctx, "SELECT COUNT(*) FROM internal_user_cache__discord WHERE id = $1", id).Scan(&count)

	if err == nil && count > 0 {
		// Check if expired
		var lastUpdated time.Time

		err = state.Pool.QueryRow(ctx, "SELECT last_updated FROM internal_user_cache__discord WHERE id = $1", id).Scan(&lastUpdated)

		if err != nil {
			return nil, err
		}

		if time.Since(lastUpdated) > userExpiryTime {
			// Update in background, since this is in cache, users won't mind this but will mind timeouts
			go func() {
				// Get from discord
				user, err := state.Discord.Session.User(id)

				if err != nil {
					state.Logger.Error("Failed to update expired user cache", zap.Error(err))
				}

				cachedReturn(&PlatformUser{
					ID:          id,
					Username:    user.Username,
					Avatar:      user.AvatarURL(""),
					DisplayName: user.GlobalName,
					Bot:         user.Bot,
					Status:      PlatformStatusOffline,
				})
			}()
		}

		var username string
		var avatar string
		var bot bool
		var createdAt time.Time
		var displayName string

		err = state.Pool.QueryRow(ctx, "SELECT username, display_name, avatar, bot, created_at FROM internal_user_cache__discord WHERE id = $1", id).Scan(&username, &displayName, &avatar, &bot, &createdAt)

		if err != nil {
			return nil, err
		}

		return cachedReturn(&PlatformUser{
			ID:          id,
			Username:    username,
			Avatar:      avatar,
			DisplayName: displayName,
			Bot:         bot,
			Status:      PlatformStatusOffline,
		})
	}

	// Get from discord
	user, err := state.Discord.Session.User(id)

	if err != nil {
		return nil, fmt.Errorf("failed to update expired user cache: %s", err)
	}

	return cachedReturn(&PlatformUser{
		ID:          id,
		Username:    user.Username,
		Avatar:      user.AvatarURL(""),
		DisplayName: user.GlobalName,
		Bot:         user.Bot,
		Status:      PlatformStatusOffline,
	})
}
