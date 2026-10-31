package httpserver

import (
	"github.com/cristiano-pacheco/bricks/pkg/http/response"
	"github.com/cristiano-pacheco/bricks/pkg/logger"
	"go.uber.org/fx"
)

// Module provides shared HTTP error rendering and the server lifecycle.
var Module = fx.Module("httpserver",
	fx.Provide(
		logger.DefaultConfig,
		fx.Annotate(logger.New, fx.As(new(logger.Logger))),
		fx.Annotate(newErrorHandler, fx.As(new(response.ErrorHandler))),
		NewAPIKeyMiddleware,
	),
	fx.Invoke(Register),
)

func newErrorHandler(applicationLogger logger.Logger) *response.ErrorHandlerImpl {
	return response.NewErrorHandler(nil, applicationLogger)
}
