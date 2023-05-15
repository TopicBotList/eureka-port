package dovewing

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/bwmarrin/discordgo"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
	"go.uber.org/zap"
)

type State struct {
	Discord        *discordgo.Session
	Logger         *zap.SugaredLogger
	PreferredGuild string
	Context        context.Context
	Pool           *pgxpool.Pool
	Redis          *redis.Client
	Popplio bool
}

var state *State

func SetState(st *State) {
	// Create the cache tables in db
	_, err := st.Pool.Exec(st.Context, `
		CREATE TABLE IF NOT EXISTS internal_user_cache (
			id TEXT PRIMARY KEY,
			username TEXT NOT NULL,
			discriminator TEXT NOT NULL,
			avatar TEXT NOT NULL,
			bot BOOLEAN NOT NULL,
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			last_updated TIMESTAMPTZ NOT NULL DEFAULT NOW()
		)
	`)

	if err != nil {
		panic(err)
	}

	state = st
}

type DiscordUser struct {
	ID            string           `json:"id" description:"The users ID"`
	Username      string           `json:"username" description:"The users username"`
	Discriminator string           `json:"discriminator" description:"The users discriminator"`
	Avatar        string           `json:"avatar" description:"The users resolved avatar URL (not just hash)"`
	Bot           bool             `json:"bot" description:"Whether the user is a bot or not"`
	Status        discordgo.Status `json:"status" description:"The users current status"`
	Nickname      string           `json:"nickname" description:"The users nickname if in a mutual server"`
	Guild         string           `json:"in_guild" description:"The guild (ID) the user is in if in a mutual server"`
}

func GetDiscordUser(ctx context.Context, id string) (userObj *DiscordUser, err error) {
	const userExpiryTime = 8 * time.Hour

	cachedReturn := func(u *DiscordUser) (*DiscordUser, error) {
		if u == nil {
			return nil, errors.New("user not found")
		}

		// Update internal_user_cache
		_, err := state.Pool.Exec(state.Context, "INSERT INTO internal_user_cache (id, username, discriminator, avatar, bot) VALUES ($1, $2, $3, $4, $5) ON CONFLICT (id) DO UPDATE SET username = $2, discriminator = $3, avatar = $4, bot = $5, last_updated = NOW()", u.ID, u.Username, u.Discriminator, u.Avatar, u.Bot)

		if err != nil {
			return nil, fmt.Errorf("failed to update internal user cache: %s", err)
		}

		// Needed for arcadia
		if u.Bot && state.Popplio {
			_, err = state.Pool.Exec(state.Context, "UPDATE bots SET queue_name = $1, queue_avatar = $2 WHERE bot_id = $3", u.Username, u.Avatar, u.ID)

			if err != nil {
				return nil, fmt.Errorf("failed to update bot queue name: %s", err)
			}
		}

		bytes, err := json.Marshal(u)

		if err == nil {
			state.Redis.Set(state.Context, "uobj:"+id, bytes, userExpiryTime)
		}

		return u, nil
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
	member, err := state.Discord.State.Member(state.PreferredGuild, id)

	if err == nil {
		p, pErr := state.Discord.State.Presence(state.PreferredGuild, id)

		if pErr != nil {
			p = &discordgo.Presence{
				User:   member.User,
				Status: discordgo.StatusOffline,
			}
		}

		return cachedReturn(&DiscordUser{
			ID:            id,
			Username:      member.User.Username,
			Avatar:        member.User.AvatarURL(""),
			Discriminator: member.User.Discriminator,
			Bot:           member.User.Bot,
			Nickname:      member.Nick,
			Guild:         state.PreferredGuild,
			Status:        p.Status,
		})
	}

	for _, guild := range state.Discord.State.Guilds {
		if guild.ID == state.PreferredGuild {
			continue // Already checked
		}

		member, err := state.Discord.State.Member(guild.ID, id)

		if err == nil {
			p, pErr := state.Discord.State.Presence(guild.ID, id)

			if pErr != nil {
				p = &discordgo.Presence{
					User:   member.User,
					Status: discordgo.StatusOffline,
				}
			}

			return cachedReturn(&DiscordUser{
				ID:            id,
				Username:      member.User.Username,
				Avatar:        member.User.AvatarURL(""),
				Discriminator: member.User.Discriminator,
				Bot:           member.User.Bot,
				Nickname:      member.Nick,
				Guild:         guild.ID,
				Status:        p.Status,
			})
		}
	}

	// Check if in redis cache
	userBytes, err := state.Redis.Get(ctx, "uobj:"+id).Result()

	if err == nil {
		// Try to unmarshal

		var user DiscordUser

		err = json.Unmarshal([]byte(userBytes), &user)

		if err == nil {
			return &user, nil
		}
	}

	// Check if in internal_user_cache, this allows fetches of users not in cache to be done in the background
	var count int64

	err = state.Pool.QueryRow(ctx, "SELECT COUNT(*) FROM internal_user_cache WHERE id = $1", id).Scan(&count)

	if err == nil && count > 0 {
		// Check if expired
		var lastUpdated time.Time

		err = state.Pool.QueryRow(ctx, "SELECT last_updated FROM internal_user_cache WHERE id = $1", id).Scan(&lastUpdated)

		if err != nil {
			return nil, err
		}

		if time.Since(lastUpdated) > userExpiryTime {
			// Update in background, since this is in cache, users won't mind this but will mind timeouts
			go func() {
				// Get from discord
				user, err := state.Discord.User(id)

				if err != nil {
					state.Logger.Error("Failed to update expired user cache", zap.Error(err))
				}

				cachedReturn(&DiscordUser{
					ID:            id,
					Username:      user.Username,
					Avatar:        user.AvatarURL(""),
					Discriminator: user.Discriminator,
					Bot:           user.Bot,
					Status:        discordgo.StatusOffline,
				})
			}()
		}

		var username string
		var discriminator string
		var avatar string
		var bot bool
		var createdAt time.Time

		err = state.Pool.QueryRow(ctx, "SELECT username, discriminator, avatar, bot, created_at FROM internal_user_cache WHERE id = $1", id).Scan(&username, &discriminator, &avatar, &bot, &createdAt)

		if err != nil {
			return nil, err
		}

		return cachedReturn(&DiscordUser{
			ID:            id,
			Username:      username,
			Avatar:        avatar,
			Discriminator: discriminator,
			Bot:           bot,
			Status:        discordgo.StatusOffline,
		})
	}

	// Get from discord
	user, err := state.Discord.User(id)

	if err != nil {
		return nil, fmt.Errorf("failed to update expired user cache: %s", err)
	}

	return cachedReturn(&DiscordUser{
		ID:            id,
		Username:      user.Username,
		Avatar:        user.AvatarURL(""),
		Discriminator: user.Discriminator,
		Bot:           user.Bot,
		Status:        discordgo.StatusOffline,
	})
}
