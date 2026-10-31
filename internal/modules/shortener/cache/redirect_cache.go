package cache

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/cristiano-pacheco/go-shortner/internal/modules/shortener/port"
	"github.com/cristiano-pacheco/go-shortner/internal/shared/infrastructure"
)

const redirectCachePrefix = "shortener:redirect:v1:"
const redirectCacheTimeout = time.Second

type RedirectCache struct {
	resources *infrastructure.Resources
}

var _ port.RedirectCache = (*RedirectCache)(nil)

func NewRedirectCache(resources *infrastructure.Resources) *RedirectCache {
	return &RedirectCache{resources: resources}
}

func (c *RedirectCache) Get(ctx context.Context, code string) (port.RedirectSnapshot, bool, error) {
	ctx, cancel := context.WithTimeout(ctx, redirectCacheTimeout)
	defer cancel()

	data, err := c.resources.Redis.Get(ctx, redirectCachePrefix+code).Bytes()
	if errors.Is(err, redis.Nil) {
		return port.RedirectSnapshot{}, false, nil
	}

	if err != nil {
		return port.RedirectSnapshot{}, false, err
	}

	var snapshot port.RedirectSnapshot
	if decodeErr := json.Unmarshal(data, &snapshot); decodeErr != nil {
		return port.RedirectSnapshot{}, false, decodeErr
	}

	return snapshot, true, nil
}

func (c *RedirectCache) Set(ctx context.Context, code string, snapshot port.RedirectSnapshot) error {
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

	return c.resources.Redis.SetArgs(ctx, redirectCachePrefix+code, data, redis.SetArgs{ExpireAt: deadline}).Err()
}

func (c *RedirectCache) Delete(ctx context.Context, code string) error {
	ctx, cancel := context.WithTimeout(ctx, redirectCacheTimeout)
	defer cancel()

	return c.resources.Redis.Del(ctx, redirectCachePrefix+code).Err()
}
