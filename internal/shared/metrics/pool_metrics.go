package metrics

import (
	"context"
	"fmt"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"go.uber.org/fx"

	"github.com/cristiano-pacheco/go-shortner/internal/shared/database"
	"github.com/cristiano-pacheco/go-shortner/internal/shared/redis"
)

// RegisterPoolMetrics collects statistics from the clients owned by the application.
//
//nolint:funlen // Each pool statistic is declared explicitly.
func RegisterPoolMetrics(
	lifecycle fx.Lifecycle,
	registerer prometheus.Registerer,
	database *database.Database,
	redisClient *redis.Client,
) {
	var registered []prometheus.Collector

	unregister := func() {
		for _, collector := range registered {
			registerer.Unregister(collector)
		}

		registered = nil
	}

	lifecycle.Append(fx.Hook{
		OnStart: func(context.Context) error {
			sqlDB, err := database.DB.DB()
			if err != nil {
				return fmt.Errorf("get PostgreSQL pool for metrics: %w", err)
			}

			poolCollectors := []prometheus.Collector{
				collectors.NewDBStatsCollector(sqlDB, "postgres"),
				prometheus.NewGaugeFunc(
					prometheus.GaugeOpts{
						Name: "shortener_redis_pool_connections",
						Help: "Redis pool total connections.",
					},
					func() float64 {
						return float64(redisClient.PoolStats().TotalConns)
					},
				),
				prometheus.NewGaugeFunc(
					prometheus.GaugeOpts{
						Name: "shortener_redis_pool_idle_connections",
						Help: "Redis pool idle connections.",
					},
					func() float64 {
						return float64(redisClient.PoolStats().IdleConns)
					},
				),
				prometheus.NewGaugeFunc(
					prometheus.GaugeOpts{
						Name: "shortener_redis_pool_pending_requests",
						Help: "Requests waiting for a Redis pool connection.",
					},
					func() float64 {
						return float64(redisClient.PoolStats().PendingRequests)
					},
				),
				prometheus.NewCounterFunc(
					prometheus.CounterOpts{
						Name: "shortener_redis_pool_hits_total",
						Help: "Redis pool acquisitions that found a free connection, not redirect cache hits.",
					},
					func() float64 {
						return float64(redisClient.PoolStats().Hits)
					},
				),
				prometheus.NewCounterFunc(
					prometheus.CounterOpts{
						Name: "shortener_redis_pool_misses_total",
						Help: "Redis pool acquisitions that did not find a free connection.",
					},
					func() float64 {
						return float64(redisClient.PoolStats().Misses)
					},
				),
				prometheus.NewCounterFunc(
					prometheus.CounterOpts{
						Name: "shortener_redis_pool_timeouts_total",
						Help: "Redis pool connection wait timeouts.",
					},
					func() float64 {
						return float64(redisClient.PoolStats().Timeouts)
					},
				),
				prometheus.NewCounterFunc(
					prometheus.CounterOpts{
						Name: "shortener_redis_pool_waits_total",
						Help: "Redis pool connection waits.",
					},
					func() float64 {
						return float64(redisClient.PoolStats().WaitCount)
					},
				),
				prometheus.NewCounterFunc(
					prometheus.CounterOpts{
						Name: "shortener_redis_pool_wait_duration_seconds_total",
						Help: "Time spent waiting for Redis pool connections.",
					},
					func() float64 {
						return float64(redisClient.PoolStats().WaitDurationNs) / float64(time.Second)
					},
				),
				prometheus.NewCounterFunc(
					prometheus.CounterOpts{
						Name: "shortener_redis_pool_unusable_connections_total",
						Help: "Unusable connections found in the Redis pool.",
					},
					func() float64 {
						return float64(redisClient.PoolStats().Unusable)
					},
				),
				prometheus.NewCounterFunc(
					prometheus.CounterOpts{
						Name: "shortener_redis_pool_stale_connections_total",
						Help: "Stale connections removed from the Redis pool.",
					},
					func() float64 {
						return float64(redisClient.PoolStats().StaleConns)
					},
				),
			}

			for _, collector := range poolCollectors {
				if registerErr := registerer.Register(collector); registerErr != nil {
					unregister()
					return fmt.Errorf("register pool metrics: %w", registerErr)
				}

				registered = append(registered, collector)
			}

			return nil
		},
		OnStop: func(context.Context) error {
			unregister()
			return nil
		},
	})
}
