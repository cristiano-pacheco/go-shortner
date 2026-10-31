package handler

import (
	"errors"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	brickserrs "github.com/cristiano-pacheco/bricks/pkg/errs"
	"github.com/cristiano-pacheco/bricks/pkg/http/response"
	"github.com/cristiano-pacheco/go-shortner/internal/modules/shortener/errs"
	"github.com/cristiano-pacheco/go-shortner/internal/modules/shortener/http/dto"
	"github.com/cristiano-pacheco/go-shortner/internal/modules/shortener/usecase"
	"github.com/cristiano-pacheco/go-shortner/internal/shared/httpserver"
)

var (
	errInvalidInput = brickserrs.New("invalid_argument", "Invalid request", http.StatusBadRequest, nil)
	errCodeConflict = brickserrs.New("code_conflict", "Code is already reserved", http.StatusConflict, nil)
	errLinkNotFound = brickserrs.New("link_not_found", "Link not found", http.StatusNotFound, nil)
)

type LinkHandler struct {
	linkCreateUseCase *usecase.LinkCreateUseCase
	linkGetUseCase    *usecase.LinkGetUseCase
	errorHandler      response.ErrorHandler
}

func NewLinkHandler(
	linkCreateUseCase *usecase.LinkCreateUseCase,
	linkGetUseCase *usecase.LinkGetUseCase,
	errorHandler response.ErrorHandler,
) *LinkHandler {
	return &LinkHandler{
		linkCreateUseCase: linkCreateUseCase,
		linkGetUseCase:    linkGetUseCase,
		errorHandler:      errorHandler,
	}
}

func (h *LinkHandler) Get(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		h.errorHandler.Error(w, errInvalidInput)
		return
	}
	output, err := h.linkGetUseCase.Execute(r.Context(), usecase.LinkGetInput{ID: id})
	if err != nil {
		switch {
		case errors.Is(err, errs.ErrInvalidInput):
			err = errInvalidInput
		case errors.Is(err, errs.ErrLinkNotFound):
			err = errLinkNotFound
		}
		h.errorHandler.Error(w, err)
		return
	}
	if err = response.JSON(
		w,
		http.StatusOK,
		dto.LinkResponse{
			ID:          output.ID,
			Code:        output.Code,
			Destination: output.Destination,
			ShortURL:    output.ShortURL,
			Active:      output.Active,
			ExpiresAt:   output.ExpiresAt,
			CreatedAt:   output.CreatedAt,
			UpdatedAt:   output.UpdatedAt,
		},
		nil,
	); err != nil {
		//nolint:sloglint // Preserve the process-wide default logger for response write failures.
		slog.ErrorContext(r.Context(), "write link retrieval response", "error", err)
	}
}

func (h *LinkHandler) Create(w http.ResponseWriter, r *http.Request) {
	var request *dto.LinkCreateRequest
	if err := httpserver.ReadJSON(r, &request); err != nil {
		h.errorHandler.Error(w, err)
		return
	}
	if request == nil {
		h.errorHandler.Error(w, errInvalidInput)
		return
	}
	output, err := h.linkCreateUseCase.Execute(
		r.Context(),
		usecase.LinkCreateInput{Destination: request.Destination, Alias: request.Alias, ExpiresAt: request.ExpiresAt},
	)
	if err != nil {
		switch {
		case errors.Is(err, errs.ErrInvalidInput):
			err = errInvalidInput
		case errors.Is(err, errs.ErrCodeConflict):
			err = errCodeConflict
		}
		h.errorHandler.Error(w, err)
		return
	}
	if err = response.JSON(
		w,
		http.StatusCreated,
		dto.LinkResponse{
			ID:          output.ID,
			Code:        output.Code,
			Destination: output.Destination,
			ShortURL:    output.ShortURL,
			Active:      output.Active,
			ExpiresAt:   output.ExpiresAt,
			CreatedAt:   output.CreatedAt,
			UpdatedAt:   output.UpdatedAt,
		},
		nil,
	); err != nil {
		//nolint:sloglint // Preserve the process-wide default logger for response write failures.
		slog.ErrorContext(r.Context(), "write link creation response", "error", err)
	}
}
