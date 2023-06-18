package dovewing

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
	"go.uber.org/zap"
)

type State struct {
	Logger   *zap.SugaredLogger
	Context  context.Context
	Pool     *pgxpool.Pool
	Redis    *redis.Client
	OnUpdate func(u *PlatformUser) error

	// internal
	initted bool
}

var state *State

// Sets global state, needed before making any call to dovewing
func SetGlobalState(st *State) {
	st.initted = true
	state = st
}

type Platform interface {
	// initializes a platform, most of the time, needs no implementation
	init() error
	// returns whether or not the platform is initialized, init() must set this to true if called
	initted() bool
	// returns the name of the platform, used for cache table names
	platformName() string
	// validate the id, if feasible
	validateId(id string) (string, error)
	// try and find the user in the platform's cache, should only hit cache
	//
	// if user not found, return nil, nil (error should be nil and user obj should be nil)
	//
	// note that returning a error here will cause the user to not be fetched from the platform
	platformSpecificCache(ctx context.Context, id string) (*PlatformUser, error)
	// fetch a user from the platform, at this point, assume that cache has been checked
	getUser(ctx context.Context, id string) (*PlatformUser, error)
}

// Common platform init code
func InitPlatform(platform Platform) error {
	var tableName = "internal_user_cache__" + platform.platformName()

	_, err := state.Pool.Exec(state.Context, `
		CREATE TABLE IF NOT EXISTS `+tableName+` (
			id TEXT PRIMARY KEY,
			username TEXT NOT NULL,
			display_name TEXT NOT NULL,
			avatar TEXT NOT NULL,
			bot BOOLEAN NOT NULL,
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			last_updated TIMESTAMPTZ NOT NULL DEFAULT NOW()
		)
	`)

	if err != nil {
		return err
	}

	return platform.init()
}

func GetUser(ctx context.Context, id string, platform Platform) (*PlatformUser, error) {
	if !state.initted {
		return nil, errors.New("state not initialized")
	}

	if !platform.initted() {
		// call InitPlatform first
		err := InitPlatform(platform)

		if err != nil {
			return nil, errors.New("failed to init platform: " + err.Error())
		}

		if !platform.initted() {
			return nil, errors.New("platform init() did not set initted() to true")
		}
	}

	var platformName = platform.platformName()
	var tableName = "internal_user_cache__" + platformName

	const userExpiryTime = 16 * time.Hour

	// Common cacher, applicable to all use cases
	cachedReturn := func(u *PlatformUser) (*PlatformUser, error) {
		if u == nil {
			return nil, errors.New("user not found")
		}

		if u.DisplayName == "" {
			u.DisplayName = u.Username
		}

		// Update internal_user_cache
		_, err := state.Pool.Exec(state.Context, "INSERT INTO "+tableName+" (id, username, display_name, avatar, bot) VALUES ($1, $2, $3, $4, $5) ON CONFLICT (id) DO UPDATE SET username = $2, display_name = $3, avatar = $4, bot = $5, last_updated = NOW()", u.ID, u.Username, u.DisplayName, u.Avatar, u.Bot)

		if err != nil {
			return nil, fmt.Errorf("failed to update internal user cache: %s", err)
		}

		if u.Bot && state.OnUpdate != nil {
			err := state.OnUpdate(u)
			if err != nil {
				return nil, fmt.Errorf("updateCache failed: %s", err)
			}
		}

		bytes, err := json.Marshal(u)

		if err == nil {
			state.Redis.Set(state.Context, "uobj__"+platformName+":"+id, bytes, userExpiryTime)
		}

		return u, nil
	}

	// First, check platform specific cache
	u, err := platform.platformSpecificCache(ctx, id)

	if err != nil {
		return nil, fmt.Errorf("platformSpecificCache failed: %s", err)
	}

	if u != nil {
		return cachedReturn(u)
	}

	// Check if in redis cache
	userBytes, err := state.Redis.Get(ctx, "uobj__"+platformName+":"+id).Result()

	if err == nil {
		// Try to unmarshal
		var user PlatformUser

		err = json.Unmarshal([]byte(userBytes), &user)

		if err == nil {
			return &user, nil
		}
	}

	// Check if in internal user cache, this allows fetches of users not in cache to be done in the background
	var count int64

	err = state.Pool.QueryRow(ctx, "SELECT COUNT(*) FROM "+tableName+" WHERE id = $1", id).Scan(&count)

	// If theres a error here, then warn and continue. We never want to fail a fetch because of a internal cache table being remade
	if err != nil {
		state.Logger.Warnf("failed to check internal user cache: %s", err)
	}

	if err == nil && count > 0 {
		// Check if expired
		var lastUpdated time.Time

		err = state.Pool.QueryRow(ctx, "SELECT last_updated FROM "+tableName+" WHERE id = $1", id).Scan(&lastUpdated)

		if err != nil {
			return nil, err
		}

		if time.Since(lastUpdated) > userExpiryTime {
			// Update in background, since this is in cache, users won't mind this but will mind timeouts
			go func() {
				// Get from platform
				state.Logger.Info("Updating expired user cache", zap.String("id", id), zap.String("platform", platformName))

				user, err := platform.getUser(ctx, id)

				if err != nil {
					state.Logger.Error("Failed to update expired user cache", zap.Error(err))
				}

				cachedReturn(&PlatformUser{
					ID:          id,
					Username:    user.Username,
					Avatar:      user.Avatar,
					DisplayName: user.DisplayName,
					Bot:         user.Bot,
					Status:      user.Status,
				})
			}()
		}

		var username string
		var avatar string
		var bot bool
		var createdAt time.Time
		var displayName string

		err = state.Pool.QueryRow(ctx, "SELECT username, display_name, avatar, bot, created_at FROM "+tableName+" WHERE id = $1", id).Scan(&username, &displayName, &avatar, &bot, &createdAt)

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

	// Get from platform
	user, err := platform.getUser(ctx, id)

	if err != nil {
		return nil, errors.New("failed to get user from platform: " + err.Error())
	}

	return cachedReturn(user)
}
