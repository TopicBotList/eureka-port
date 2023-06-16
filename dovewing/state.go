package dovewing

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
	"go.uber.org/zap"
)

type State struct {
	Discord        *DiscordState
	Logger         *zap.SugaredLogger
	PreferredGuild string
	Context        context.Context
	Pool           *pgxpool.Pool
	Redis          *redis.Client
}

var state *State

func SetState(st *State) {
	// Create the cache tables in db
	_, err := st.Pool.Exec(st.Context, `
		CREATE TABLE IF NOT EXISTS internal_user_cache__discord (
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
		panic(err)
	}

	state = st
}
