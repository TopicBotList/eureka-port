package dovewing

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/infinitybotlist/eureka/dovewing/dovetypes"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
	"go.uber.org/zap"
	"golang.org/x/exp/slices"
)

type BaseState struct {
	Logger         *zap.Logger
	Context        context.Context
	Pool           *pgxpool.Pool
	Redis          *redis.Client
	Middlewares    []func(p Platform, u *dovetypes.PlatformUser) (*dovetypes.PlatformUser, error)
	UserExpiryTime time.Duration
}

type Platform interface {
	// initializes a platform, most of the time, needs no implementation
	Init() error
	// returns whether or not the platform is initialized, init() must set this to true if called
	Initted() bool
	// Returns the base state
	GetState() *BaseState
	// returns the name of the platform, used for cache table names
	PlatformName() string
	// validate the id, if feasible
	ValidateId(id string) (string, error)
	// try and find the user in the platform's cache, should only hit cache
	//
	// if user not found, return nil, nil (error should be nil and user obj should be nil)
	//
	// note that returning a error here will cause the user to not be fetched from the platform
	PlatformSpecificCache(ctx context.Context, id string) (*dovetypes.PlatformUser, error)
	// fetch a user from the platform, at this point, assume that cache has been checked
	GetUser(ctx context.Context, id string) (*dovetypes.PlatformUser, error)
}

// Common platform init code
func InitPlatform(platform Platform) error {
	state := platform.GetState()

	var tableName = TableName(platform)

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

	return platform.Init()
}

// Returns the table name of a platform
func TableName(platform Platform) string {
	return "internal_user_cache__" + platform.PlatformName()
}

// Fetches a user based on the platform
func GetUser(ctx context.Context, id string, platform Platform) (*dovetypes.PlatformUser, error) {
	state := platform.GetState()

	if !platform.Initted() {
		// call InitPlatform first
		err := InitPlatform(platform)

		if err != nil {
			return nil, errors.New("failed to init platform: " + err.Error())
		}

		if !platform.Initted() {
			return nil, errors.New("platform init() did not set initted() to true")
		}
	}

	var platformName = platform.PlatformName()
	var tableName = TableName(platform)

	// Common cacher, applicable to all use cases
	cachedReturn := func(u *dovetypes.PlatformUser) (*dovetypes.PlatformUser, error) {
		if u == nil {
			return nil, errors.New("user not found")
		}

		if u.DisplayName == "" {
			u.DisplayName = u.Username
		}

		var err error

		for i, middleware := range state.Middlewares {
			u, err = middleware(platform, u)

			if err != nil {
				return nil, fmt.Errorf("middleware %d failed: %s", i, err)
			}
		}

		// Update cache
		_, err = state.Pool.Exec(state.Context, "INSERT INTO "+tableName+" (id, username, display_name, avatar, bot) VALUES ($1, $2, $3, $4, $5) ON CONFLICT (id) DO UPDATE SET username = $2, display_name = $3, avatar = $4, bot = $5, last_updated = NOW()", u.ID, u.Username, u.DisplayName, u.Avatar, u.Bot)

		if err != nil {
			return nil, fmt.Errorf("failed to update internal user cache: %s", err)
		}

		bytes, err := json.Marshal(u)

		if err == nil {
			state.Redis.Set(state.Context, "uobj__"+platformName+":"+id, bytes, state.UserExpiryTime)
		}

		return u, nil
	}

	// First, check platform specific cache
	uCached, err := platform.PlatformSpecificCache(ctx, id)

	if err != nil {
		return nil, fmt.Errorf("platformSpecificCache failed: %s", err)
	}

	if uCached != nil {
		return cachedReturn(uCached)
	}

	// Check if in redis cache
	userBytes, err := state.Redis.Get(ctx, "uobj__"+platformName+":"+id).Result()

	if err == nil {
		// Try to unmarshal
		var user dovetypes.PlatformUser

		err = json.Unmarshal([]byte(userBytes), &user)

		if err == nil {
			user.ExtraData = map[string]any{
				"cache": "redis",
			}
			return &user, nil
		}
	}

	// Check if in internal user cache, this allows fetches of users not in cache to be done in the background
	var count int64

	err = state.Pool.QueryRow(ctx, "SELECT COUNT(*) FROM "+tableName+" WHERE id = $1", id).Scan(&count)

	if errors.Is(err, pgx.ErrNoRows) {
		count = 0
	} else if err != nil {
		// If theres a error here, then warn and continue. We never want to fail a fetch because of a internal cache table being remade
		state.Logger.Warn("Failed to check internal user cache.", zap.Error(err), zap.String("id", id), zap.String("platform", platformName), zap.String("tableName", tableName))
	}

	if err == nil && count > 0 {
		// Check if expired
		var lastUpdated time.Time

		err = state.Pool.QueryRow(ctx, "SELECT last_updated FROM "+tableName+" WHERE id = $1", id).Scan(&lastUpdated)

		if err != nil {
			return nil, err
		}

		if time.Since(lastUpdated) > state.UserExpiryTime {
			// Update in background, since this is in cache, users won't mind this but will mind timeouts
			go func() {
				// Get from platform
				state.Logger.Info("Updating expired user cache", zap.String("id", id), zap.String("platform", platformName))

				user, err := platform.GetUser(ctx, id)

				if err != nil {
					state.Logger.Error("Failed to update expired user cache", zap.Error(err))
					return
				}

				cachedReturn(&dovetypes.PlatformUser{
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

		return cachedReturn(&dovetypes.PlatformUser{
			ID:          id,
			Username:    username,
			Avatar:      avatar,
			DisplayName: displayName,
			Bot:         bot,
			Status:      dovetypes.PlatformStatusOffline,
			ExtraData: map[string]any{
				"cache": "pg",
			},
		})
	}

	// Get from platform
	user, err := platform.GetUser(ctx, id)

	if err != nil {
		return nil, errors.New("failed to get user from platform: " + err.Error())
	}

	return cachedReturn(user)
}

type ClearFrom string

const (
	ClearFromInternalUserCache ClearFrom = "iuc"
	ClearFromRedis             ClearFrom = "redis"
)

// ClearUserInfo contains information on a clear operation
type ClearUserInfo struct {
	// The user that was cleared
	ClearedFrom []ClearFrom
}

type ClearUserReq struct {
	// Where to clear from
	//
	// iuc -> internal user cache (postgres)
	//
	// Redis -> redis cache
	//
	//
	// If not specified, will clear from all
	ClearFrom []ClearFrom
}

// Clears a user of a platform
func ClearUser(ctx context.Context, id string, platform Platform, req ClearUserReq) (*ClearUserInfo, error) {
	state := platform.GetState()

	if !platform.Initted() {
		// call InitPlatform first
		err := InitPlatform(platform)

		if err != nil {
			return nil, errors.New("failed to init platform: " + err.Error())
		}

		if !platform.Initted() {
			return nil, errors.New("platform init() did not set initted() to true")
		}
	}

	var platformName = platform.PlatformName()
	var tableName = TableName(platform)

	var clearedFrom []ClearFrom

	// Check iuc
	if len(req.ClearFrom) == 0 || slices.Contains(req.ClearFrom, ClearFromInternalUserCache) {
		var count int64

		err := state.Pool.QueryRow(ctx, "SELECT COUNT(*) FROM "+tableName+" WHERE id = $1", id).Scan(&count)

		if err != nil {
			return nil, err
		}

		if count > 0 {
			// Delete from iuc
			_, err = state.Pool.Exec(ctx, "DELETE FROM "+tableName+" WHERE id = $1", id)

			if err != nil {
				return nil, err
			}

			clearedFrom = append(clearedFrom, ClearFromInternalUserCache)
		}
	}

	// Check redis
	if len(req.ClearFrom) == 0 || slices.Contains(req.ClearFrom, ClearFromRedis) {
		// Delete from redis
		_, err := state.Redis.Del(ctx, "uobj__"+platformName+":"+id).Result()

		if err != nil {
			return nil, err
		}

		clearedFrom = append(clearedFrom, ClearFromRedis) // TODO: make this a constant
	}

	return &ClearUserInfo{
		ClearedFrom: clearedFrom,
	}, nil
}
