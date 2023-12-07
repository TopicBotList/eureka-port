package hotcache

import (
	"context"
	"errors"
	"time"
)

// A HotCache represents a cache that is hot (meaning that it is used often)
//
// Dovewing provides a redis cache for you, but you can implement your own if you want
type HotCache[T any] interface {
	// Get a value from the cache
	Get(ctx context.Context, key string) (*T, error)

	// Delete a value from the cache
	Delete(ctx context.Context, key string) error

	// Set a value in the cache
	Set(ctx context.Context, key string, value *T, expiry time.Duration) error
}

var ErrHotCacheDataNotFound = errors.New("hot cache data not found")
