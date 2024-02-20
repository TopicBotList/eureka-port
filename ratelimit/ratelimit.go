// Ratelimit implementation
package ratelimit

import (
	"context"
	"crypto/sha256"
	"fmt"
	"net/http"
	"strconv"
	"time"
	"errors"

	"github.com/topicbotlist/eureka-port/hotcache"
)

var zero = 0

type RLState struct {
	HotCache hotcache.HotCache[int]
}

var State *RLState

func SetupState(s *RLState) {
	State = s
}

type Ratelimit struct {
	// Expiry is the time for the ratelimit to expire
	Expiry time.Duration
	// MaxRequests is the maximum number of requests allowed in the interval specified by Expiry for the bucket
	MaxRequests int
	// Bucket is the bucket to use for the ratelimit
	Bucket string
	// Identifier is the identifier of the ratelimit, otherwise DefaultIdentifier is used
	Identifier func(r *http.Request) string
}

// Limit is used to check if the ratelimit has been exceeded
type Limit struct {
	// Exceeded is true if the ratelimit has been exceeded
	Exceeded bool
	// Made is the number of requests made in the ratelimit
	Made int
	// Remaining is the number of requests remaining in the ratelimit
	Remaining int
	// TimeToReset is the time remaining until the ratelimit resets
	TimeToReset time.Duration
	// GotIdentifier is the identifier of the ratelimit
	GotIdentifier string
	// MaxRequests is the maximum number of requests allowed in the interval specified by Expiry for the bucket
	MaxRequests int
	// Bucket is the bucket to use for the ratelimit
	Bucket string
}

func (l Limit) Headers() map[string]string {
	if l.Exceeded {
		return map[string]string{
			"Retry-After": strconv.FormatFloat(l.TimeToReset.Seconds(), 'f', -1, 64),
			"Req-Made":    strconv.Itoa(l.Made),
			"Req-Limit":   strconv.Itoa(l.MaxRequests),
			"Bucket":      l.Bucket,
		}
	}

	return map[string]string{
		"Req-Made":  strconv.Itoa(l.Made),
		"Req-Limit": strconv.Itoa(l.MaxRequests),
		"Bucket":    l.Bucket,
	}
}

func (rl Ratelimit) Limit(ctx context.Context, r *http.Request) (Limit, error) {
	if rl.Identifier == nil {
		rl.Identifier = DefaultIdentifier
	}

	// Hash the identifier for privacy
	identifier := fmt.Sprintf("%x", sha256.Sum256([]byte(rl.Identifier(r))))

	// Check if rate even exists
	exists, err := State.HotCache.Exists(ctx, rl.Bucket+"-"+identifier)

	if err != nil {
		return Limit{GotIdentifier: identifier}, err
	}

	// If the rate doesn't exist, set it
	if !exists {
		err = State.HotCache.Set(ctx, rl.Bucket+"-"+identifier, &zero, rl.Expiry)

		if err != nil {
			return Limit{GotIdentifier: identifier}, err
		}
	}

	// Get the current rate from redis
	currentRate, err := State.HotCache.Get(ctx, rl.Bucket+"-"+identifier)

	if errors.Is(err, hotcache.ErrHotCacheDataNotFound) {
		rateDefault := 0
		currentRate = &rateDefault
	} else if err != nil {
		return Limit{GotIdentifier: identifier}, err
	}

	// Check if the rate has been exceeded
	exceeded := *currentRate > rl.MaxRequests

	// Increment the rate
	err = State.HotCache.IncrementOne(ctx, rl.Bucket+"-"+identifier)

	if err != nil {
		return Limit{GotIdentifier: identifier}, err
	}

	// Get the time when the rate will reset
	resetTime, err := State.HotCache.Expiry(ctx, rl.Bucket+"-"+identifier)

	if err != nil {
		return Limit{GotIdentifier: identifier}, err
	}

	return Limit{
		GotIdentifier: identifier,
		Exceeded:      exceeded,
		Made:          *currentRate,
		TimeToReset:   resetTime,
		MaxRequests:   rl.MaxRequests,
		Bucket:        rl.Bucket,
	}, nil
}

func DefaultIdentifier(r *http.Request) string {
	return r.RemoteAddr
}
