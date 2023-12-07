package hotcache

import (
	"context"
	"errors"
	"time"
)

// A HotCache represents a cache that is hot (meaning that it is used often)
//
// Eureka provides a redis cache for you, but you can implement your own if you want
type HotCache[T any] interface {
	// Get a value from the cache
	Get(ctx context.Context, key string) (*T, error)

	// Delete a value from the cache
	Delete(ctx context.Context, key string) error

	// Set a value in the cache
	Set(ctx context.Context, key string, value *T, expiry time.Duration) error

	// Increment a value in the cache
	Increment(ctx context.Context, key string, value int64) error

	// Increment by one a value in the cache
	//
	// This can be faster than Increment(ctx, key, 1)
	IncrementOne(ctx context.Context, key string) error

	// Checks if a value exists in the cache
	Exists(ctx context.Context, key string) (bool, error)

	// Checks the expiry of a value in the cache
	Expiry(ctx context.Context, key string) (time.Duration, error)
}

var ErrHotCacheDataNotFound = errors.New("hot cache data not found")
