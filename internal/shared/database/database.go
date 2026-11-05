package database

import (
	"context"
	"net"
	"net/url"
	"time"

	"go.uber.org/fx"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"github.com/cristiano-pacheco/go-shortner/internal/shared/config"
	sharederrs "github.com/cristiano-pacheco/go-shortner/internal/shared/errs"
)

const (
	maxOpenConnections  = 10
	maxIdleConnections  = 5
	postgresPingTimeout = 5 * time.Second
)

type Database struct {
	*gorm.DB
}

func NewPostgreSQL(lifecycle fx.Lifecycle, cfg config.Config) *Database {
	database := &Database{}

	lifecycle.Append(fx.Hook{
		OnStart: func(ctx context.Context) error {
			dsn := &url.URL{
				Scheme: "postgres",
				User:   url.UserPassword(cfg.DatabaseUser, cfg.DatabasePassword),
				Host:   net.JoinHostPort(cfg.DatabaseHost, cfg.DatabasePort),
				Path:   "/" + cfg.DatabaseName,
			}
			query := dsn.Query()
			query.Set("sslmode", cfg.DatabaseSSLMode)
			query.Set("statement_timeout", "5000")
			query.Set("connect_timeout", "3")
			query.Set("TimeZone", "UTC")
			dsn.RawQuery = query.Encode()

			db, err := gorm.Open(postgres.Open(dsn.String()), &gorm.Config{
				DisableAutomaticPing: true,
				TranslateError:       true,
				Logger:               logger.Default.LogMode(logger.Silent),
			})
			if err != nil {
				return sharederrs.ErrPostgreSQLInit
			}

			sqlDB, err := db.DB()
			if err != nil {
				return sharederrs.ErrPostgreSQLPoolInit
			}

			sqlDB.SetMaxOpenConns(maxOpenConnections)
			sqlDB.SetMaxIdleConns(maxIdleConnections)

			pingCtx, cancel := context.WithTimeout(ctx, postgresPingTimeout)
			defer cancel()

			if pingErr := sqlDB.PingContext(pingCtx); pingErr != nil {
				_ = sqlDB.Close()
				return sharederrs.ErrPostgreSQLConnect
			}

			database.DB = db

			return nil
		},
		OnStop: func(context.Context) error {
			sqlDB, err := database.DB.DB()
			if err != nil {
				return err
			}

			return sqlDB.Close()
		},
	})

	return database
}
