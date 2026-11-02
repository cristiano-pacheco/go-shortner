package redis

import (
	"context"
	"errors"
	"time"

	"github.com/redis/go-redis/v9"
	"go.uber.org/fx"

	"github.com/cristiano-pacheco/go-shortner/internal/shared/config"
)

const redisPingTimeout = 3 * time.Second

type Client struct {
	*redis.Client
}

func NewRedis(lifecycle fx.Lifecycle, cfg config.Config) (*Client, error) {
	opts, err := redis.ParseURL(cfg.RedisURL)
	if err != nil {
		return nil, errors.New("invalid Redis URL")
	}

	opts.DialTimeout = time.Second
	opts.ReadTimeout = time.Second
	opts.WriteTimeout = time.Second
	opts.PoolTimeout = time.Second
	opts.MaxRetries = -1
	opts.ContextTimeoutEnabled = true
	client := &Client{}

	lifecycle.Append(fx.Hook{
		OnStart: func(ctx context.Context) error {
			redisClient := redis.NewClient(opts)

			pingCtx, cancel := context.WithTimeout(ctx, redisPingTimeout)
			defer cancel()

			if pingErr := redisClient.Ping(pingCtx).Err(); pingErr != nil {
				_ = redisClient.Close()
				return errors.New("cannot connect to Redis")
			}

			client.Client = redisClient

			return nil
		},
		OnStop: func(context.Context) error {
			return client.Client.Close()
		},
	})

	return client, nil
}
