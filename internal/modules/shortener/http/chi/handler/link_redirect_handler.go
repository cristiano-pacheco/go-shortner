package handler

import (
	"errors"
	"net/http"

	"github.com/cristiano-pacheco/bricks/pkg/http/response"
	"github.com/go-chi/chi/v5"
	playgroundvalidator "github.com/go-playground/validator/v10"

	"github.com/cristiano-pacheco/go-shortner/internal/modules/shortener/errs"
	"github.com/cristiano-pacheco/go-shortner/internal/modules/shortener/usecase"
)

type LinkRedirectHandler struct {
	linkResolveUseCase *usecase.LinkResolveUseCase
	errorHandler       response.ErrorHandler
}

func NewLinkRedirectHandler(
	linkResolveUseCase *usecase.LinkResolveUseCase,
	errorHandler response.ErrorHandler,
) *LinkRedirectHandler {
	return &LinkRedirectHandler{linkResolveUseCase: linkResolveUseCase, errorHandler: errorHandler}
}

func (h *LinkRedirectHandler) Redirect(w http.ResponseWriter, r *http.Request) {
	output, err := h.linkResolveUseCase.Execute(r.Context(), usecase.LinkResolveInput{Code: chi.URLParam(r, "code")})
	if err != nil {
		var validationErrors playgroundvalidator.ValidationErrors
		if errors.Is(err, errs.ErrLinkNotFound) || errors.As(err, &validationErrors) {
			err = errLinkNotFound
		}

		h.errorHandler.Error(w, err)

		return
	}

	w.Header().Set("Location", output.Destination)
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusFound)
}
