package application

import (
	bricksmetrics "github.com/cristiano-pacheco/bricks/pkg/http/server/metrics"
	bricksvalidator "github.com/cristiano-pacheco/bricks/pkg/validator"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"go.uber.org/fx"

	"github.com/cristiano-pacheco/go-shortner/internal/modules/shortener"
	"github.com/cristiano-pacheco/go-shortner/internal/shared/config"
	"github.com/cristiano-pacheco/go-shortner/internal/shared/database"
	"github.com/cristiano-pacheco/go-shortner/internal/shared/httpserver"
	"github.com/cristiano-pacheco/go-shortner/internal/shared/redis"
)

// Module composes the server and its dependencies.
var Module = fx.Options(
	fx.Provide(config.Load),
	bricksvalidator.Module,
	fx.Provide(fx.Annotate(newMetricsRegistry,
		fx.As(new(prometheus.Gatherer)),
		fx.As(new(prometheus.Registerer)),
	)),
	bricksmetrics.Module,
	database.Module,
	redis.Module,
	shortener.Module,
	httpserver.Module,
)

func newMetricsRegistry() (*prometheus.Registry, error) {
	registry := prometheus.NewRegistry()
	if err := registry.Register(collectors.NewGoCollector()); err != nil {
		return nil, err
	}

	if err := registry.Register(collectors.NewProcessCollector(collectors.ProcessCollectorOpts{})); err != nil {
		return nil, err
	}

	return registry, nil
}
