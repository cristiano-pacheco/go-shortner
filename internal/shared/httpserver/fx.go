package httpserver

import (
	"github.com/cristiano-pacheco/bricks/pkg/http/response"
	"github.com/cristiano-pacheco/bricks/pkg/http/server/chi"
	"github.com/cristiano-pacheco/bricks/pkg/logger"
	"go.uber.org/fx"
)

// Module provides shared HTTP error rendering and application routes.
var Module = fx.Module("httpserver",
	fx.Provide(
		fx.Annotate(logger.New, fx.As(new(logger.Logger))),
		fx.Annotate(newErrorHandler, fx.As(new(response.ErrorHandler))),
		NewAPIKeyMiddleware,
		NewMetricsMiddleware,
		fx.Annotate(newApplicationRoutes, fx.As(new(chi.Route)), fx.ResultTags(`group:"routes"`)),
	),
)

func newErrorHandler(applicationLogger logger.Logger) *response.ErrorHandlerImpl {
	return response.NewErrorHandler(nil, applicationLogger)
}
