package router

import (
	"github.com/cristiano-pacheco/go-shortner/internal/modules/shortener/http/chi/handler"
	"github.com/cristiano-pacheco/go-shortner/internal/shared/httpserver"
	"github.com/go-chi/chi/v5"
)

type LinkRouter struct{ handler *handler.LinkHandler }

func NewLinkRouter(handler *handler.LinkHandler) *LinkRouter { return &LinkRouter{handler: handler} }

func (r *LinkRouter) Register(router chi.Router) { router.Post("/links", r.handler.Create) }

func (r *LinkRouter) ManagementRoute() httpserver.ManagementRoute { return r.Register }
