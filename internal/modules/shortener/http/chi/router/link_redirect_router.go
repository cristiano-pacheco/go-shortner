package router

import (
	"github.com/go-chi/chi/v5"

	"github.com/cristiano-pacheco/go-shortner/internal/modules/shortener/http/chi/handler"
	"github.com/cristiano-pacheco/go-shortner/internal/shared/httpserver"
)

type LinkRedirectRouter struct {
	linkRedirectHandler *handler.LinkRedirectHandler
}

func NewLinkRedirectRouter(linkRedirectHandler *handler.LinkRedirectHandler) *LinkRedirectRouter {
	return &LinkRedirectRouter{linkRedirectHandler: linkRedirectHandler}
}

func (r *LinkRedirectRouter) Register(router chi.Router) {
	router.Get("/{code}", r.linkRedirectHandler.Redirect)
	router.Head("/{code}", r.linkRedirectHandler.Redirect)
}

func (r *LinkRedirectRouter) PublicRoute() httpserver.PublicRoute {
	return r.Register
}
