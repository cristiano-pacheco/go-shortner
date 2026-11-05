package httpserver

import (
	"github.com/cristiano-pacheco/bricks/pkg/http/response"
	"github.com/cristiano-pacheco/bricks/pkg/http/server/chi"
	"github.com/cristiano-pacheco/bricks/pkg/logger"
	"go.uber.org/fx"
)

var sharedModule = fx.Provide(
	fx.Annotate(logger.New, fx.As(new(logger.Logger))),
	fx.Annotate(newErrorHandler, fx.As(new(response.ErrorHandler))),
	NewMetricsMiddleware,
)

// PublicModule installs public routes without constructing management authentication.
var PublicModule = fx.Module("httpserver-public", sharedModule,
	fx.Provide(fx.Annotate(newApplicationRoutes, fx.As(new(chi.Route)), fx.ResultTags(`group:"routes"`))),
)

// ManagementModule installs only authenticated management routes.
var ManagementModule = fx.Module("httpserver-management", sharedModule,
	fx.Provide(
		NewAPIKeyMiddleware,
		fx.Annotate(newManagementApplicationRoutes, fx.As(new(chi.Route)), fx.ResultTags(`group:"routes"`)),
	),
)

func newErrorHandler(applicationLogger logger.Logger) *response.ErrorHandlerImpl {
	return response.NewErrorHandler(nil, applicationLogger)
}
