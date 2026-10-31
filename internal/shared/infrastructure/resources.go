package infrastructure

import (
	"context"
	"errors"
	"net"
	"net/url"
	"time"

	"github.com/redis/go-redis/v9"
	"go.uber.org/fx"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"github.com/cristiano-pacheco/go-shortner/internal/shared/config"
)

const (
	maxOpenConnections  = 10
	maxIdleConnections  = 5
	postgresPingTimeout = 5 * time.Second
	redisPingTimeout    = 3 * time.Second
)

type Resources struct {
	DB    *gorm.DB
	Redis *redis.Client
}

func NewResources(lc fx.Lifecycle, cfg config.Config) *Resources {
	r := &Resources{}

	lc.Append(fx.Hook{
		OnStart: func(ctx context.Context) error {
			dsn := &url.URL{
				Scheme: "postgres",
				User:   url.UserPassword(cfg.DatabaseUser, cfg.DatabasePassword),
				Host:   net.JoinHostPort(cfg.DatabaseHost, cfg.DatabasePort),
				Path:   "/" + cfg.DatabaseName,
			}
			q := dsn.Query()
			q.Set("sslmode", cfg.DatabaseSSLMode)
			q.Set("statement_timeout", "5000")
			q.Set("connect_timeout", "3")
			q.Set("TimeZone", "UTC")
			dsn.RawQuery = q.Encode()

			db, err := gorm.Open(
				postgres.Open(dsn.String()),
				&gorm.Config{
					DisableAutomaticPing: true,
					TranslateError:       true,
					Logger:               logger.Default.LogMode(logger.Silent),
				},
			)
			if err != nil {
				return errors.New("cannot initialize PostgreSQL")
			}

			sqlDB, err := db.DB()
			if err != nil {
				return errors.New("cannot initialize PostgreSQL pool")
			}

			sqlDB.SetMaxOpenConns(maxOpenConnections)
			sqlDB.SetMaxIdleConns(maxIdleConnections)

			pingCtx, cancel := context.WithTimeout(ctx, postgresPingTimeout)
			err = sqlDB.PingContext(pingCtx)

			cancel()

			if err != nil {
				_ = sqlDB.Close()
				return errors.New("cannot connect to PostgreSQL")
			}

			opts, err := redis.ParseURL(cfg.RedisURL)
			if err != nil {
				_ = sqlDB.Close()
				return errors.New("invalid Redis URL")
			}

			opts.DialTimeout = time.Second
			opts.ReadTimeout = time.Second
			opts.WriteTimeout = time.Second
			opts.PoolTimeout = time.Second
			opts.MaxRetries = -1
			opts.ContextTimeoutEnabled = true
			client := redis.NewClient(opts)
			pingCtx, cancel = context.WithTimeout(ctx, redisPingTimeout)
			err = client.Ping(pingCtx).Err()

			cancel()

			if err != nil {
				_ = client.Close()
				_ = sqlDB.Close()

				return errors.New("cannot connect to Redis")
			}

			r.DB = db
			r.Redis = client

			return nil
		},
		OnStop: func(context.Context) error {
			err := r.Redis.Close()

			sqlDB, dbErr := r.DB.DB()
			if dbErr == nil {
				dbErr = sqlDB.Close()
			}

			return errors.Join(err, dbErr)
		},
	})

	return r
}
