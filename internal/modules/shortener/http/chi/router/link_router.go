package router

import (
	"github.com/go-chi/chi/v5"

	"github.com/cristiano-pacheco/go-shortner/internal/modules/shortener/http/chi/handler"
	"github.com/cristiano-pacheco/go-shortner/internal/shared/httpserver"
)

type LinkRouter struct{ handler *handler.LinkHandler }

func NewLinkRouter(handler *handler.LinkHandler) *LinkRouter { return &LinkRouter{handler: handler} }

func (r *LinkRouter) Register(router chi.Router) {
	router.Post("/links", r.handler.Create)
	router.Get("/links", r.handler.List)
	router.Get("/links/{id}", r.handler.Get)
}

func (r *LinkRouter) ManagementRoute() httpserver.ManagementRoute { return r.Register }
