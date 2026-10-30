package httpserver

import (
	"github.com/cristiano-pacheco/bricks/pkg/http/response"
	"go.uber.org/fx"
)

// Module provides shared HTTP error rendering and the server lifecycle.
var Module = fx.Module("httpserver",
	fx.Provide(
		fx.Annotate(newErrorHandler, fx.As(new(response.ErrorHandler))),
		NewAPIKeyMiddleware,
	),
	fx.Invoke(Register),
)

func newErrorHandler() *response.ErrorHandlerImpl {
	return response.NewErrorHandler(nil, nil)
}
