package redis

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/topicbotlist/eureka-port/hotcache"
	"github.com/redis/go-redis/v9"
)

type RedisHotCache[T any] struct {
	Redis  *redis.Client
	Prefix string
}

func (r RedisHotCache[T]) Get(ctx context.Context, key string) (*T, error) {
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

func (r RedisHotCache[T]) Delete(ctx context.Context, key string) error {
	return r.Redis.Del(ctx, r.Prefix+key).Err()
}

func (r RedisHotCache[T]) Set(ctx context.Context, key string, value *T, expiry time.Duration) error {
	bytes, err := json.Marshal(value)

	if err != nil {
		return err
	}

	return r.Redis.Set(ctx, r.Prefix+key, bytes, expiry).Err()
}

func (r RedisHotCache[T]) Increment(ctx context.Context, key string, value int64) error {
	return r.Redis.IncrBy(ctx, r.Prefix+key, value).Err()
}

func (r RedisHotCache[T]) IncrementOne(ctx context.Context, key string) error {
	return r.Redis.Incr(ctx, r.Prefix+key).Err()
}

func (r RedisHotCache[T]) Exists(ctx context.Context, key string) (bool, error) {
	b, err := r.Redis.Exists(ctx, r.Prefix+key).Result()

	if err != nil {
		return false, err
	}

	return b > 0, nil
}

func (r RedisHotCache[T]) Expiry(ctx context.Context, key string) (time.Duration, error) {
	return r.Redis.TTL(ctx, r.Prefix+key).Result()
}
