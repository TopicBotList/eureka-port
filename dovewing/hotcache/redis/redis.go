package redis

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/infinitybotlist/eureka/dovewing/hotcache"
	"github.com/redis/go-redis/v9"
)

type RedisHotCache[T any] struct {
	Redis  *redis.Client
	Prefix string
}

func (r *RedisHotCache[T]) Get(ctx context.Context, key string) (*T, error) {
	bytes, err := r.Redis.Get(ctx, r.Prefix+key).Bytes()

	if errors.Is(err, redis.Nil) {
		return nil, hotcache.ErrHotCacheDataNotFound
	}

	if err != nil {
		return nil, err
	}

	var val T

	err = json.Unmarshal(bytes, &val)

	if err != nil {
		return nil, err
	}

	return &val, nil
}

func (r *RedisHotCache[T]) Delete(ctx context.Context, key string) error {
	return r.Redis.Del(ctx, r.Prefix+key).Err()
}

func (r *RedisHotCache[T]) Set(ctx context.Context, key string, value *T, expiry time.Duration) error {
	bytes, err := json.Marshal(value)

	if err != nil {
		return err
	}

	return r.Redis.Set(ctx, r.Prefix+key, bytes, expiry).Err()
}
