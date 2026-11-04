package cache

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/cristiano-pacheco/bricks/pkg/otel/trace"

	"github.com/redis/go-redis/v9"

	"github.com/cristiano-pacheco/go-shortner/internal/modules/shortener/errs"
	"github.com/cristiano-pacheco/go-shortner/internal/modules/shortener/port"
	"github.com/cristiano-pacheco/go-shortner/internal/shared/metrics"
	sharedredis "github.com/cristiano-pacheco/go-shortner/internal/shared/redis"
)

const redirectCachePrefix = "shortener:redirect:v1:"
const redirectCacheTimeout = time.Second

type RedirectCache struct {
	redisClient        *sharedredis.Client
	applicationMetrics *metrics.Metrics
}

var _ port.RedirectCache = (*RedirectCache)(nil)

func NewRedirectCache(redisClient *sharedredis.Client, applicationMetrics *metrics.Metrics) *RedirectCache {
	return &RedirectCache{redisClient: redisClient, applicationMetrics: applicationMetrics}
}

func (c *RedirectCache) Get(ctx context.Context, code string) (port.RedirectSnapshot, bool, error) {
	ctx, span := trace.Span(ctx, "RedirectCache.Get")
	defer span.End()

	ctx, cancel := context.WithTimeout(ctx, redirectCacheTimeout)
	defer cancel()

	startedAt := time.Now()
	data, err := c.redisClient.Get(ctx, redirectCachePrefix+code).Bytes()

	result := metrics.CacheResultSuccess
	if errors.Is(err, redis.Nil) {
		result = "miss"
	} else if err != nil {
		result = metrics.CacheResultError
	}

	c.applicationMetrics.ObserveCacheOperation("get", result, startedAt)

	if errors.Is(err, redis.Nil) {
		return port.RedirectSnapshot{}, false, nil
	}

	if err != nil {
		return port.RedirectSnapshot{}, false, err
	}

	var snapshot port.RedirectSnapshot
	if decodeErr := json.Unmarshal(data, &snapshot); decodeErr != nil {
		return port.RedirectSnapshot{}, false, errs.ErrInvalidSnapshot
	}

	return snapshot, true, nil
}

func (c *RedirectCache) Set(ctx context.Context, code string, snapshot port.RedirectSnapshot) error {
	ctx, span := trace.Span(ctx, "RedirectCache.Set")
	defer span.End()

	deadline := snapshot.FreshUntil
	if snapshot.ExpiresAt != nil && snapshot.ExpiresAt.Before(deadline) {
		deadline = *snapshot.ExpiresAt
	}

	if !deadline.After(time.Now()) {
		return nil
	}

	data, err := json.Marshal(snapshot)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(ctx, redirectCacheTimeout)
	defer cancel()

	startedAt := time.Now()
	err = c.redisClient.SetArgs(ctx, redirectCachePrefix+code, data, redis.SetArgs{ExpireAt: deadline}).Err()

	result := metrics.CacheResultSuccess
	if err != nil {
		result = metrics.CacheResultError
	}

	c.applicationMetrics.ObserveCacheOperation("set", result, startedAt)

	return err
}

func (c *RedirectCache) Delete(ctx context.Context, code string) error {
	ctx, span := trace.Span(ctx, "RedirectCache.Delete")
	defer span.End()

	ctx, cancel := context.WithTimeout(ctx, redirectCacheTimeout)
	defer cancel()

	startedAt := time.Now()
	err := c.redisClient.Del(ctx, redirectCachePrefix+code).Err()

	result := metrics.CacheResultSuccess
	if err != nil {
		result = metrics.CacheResultError
	}

	c.applicationMetrics.ObserveCacheOperation("delete", result, startedAt)

	return err
}
