package handler

import (
	"errors"
	"net/http"
	"net/url"
	"strconv"

	"github.com/go-chi/chi/v5"

	brickserrs "github.com/cristiano-pacheco/bricks/pkg/errs"
	"github.com/cristiano-pacheco/bricks/pkg/http/response"
	"github.com/cristiano-pacheco/bricks/pkg/logger"

	"github.com/cristiano-pacheco/go-shortner/internal/modules/shortener/errs"
	"github.com/cristiano-pacheco/go-shortner/internal/modules/shortener/http/dto"
	"github.com/cristiano-pacheco/go-shortner/internal/modules/shortener/http/mapper"
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
	linkListUseCase   *usecase.LinkListUseCase
	errorHandler      response.ErrorHandler
	applicationLogger logger.Logger
}

func NewLinkHandler(
	linkCreateUseCase *usecase.LinkCreateUseCase,
	linkGetUseCase *usecase.LinkGetUseCase,
	linkListUseCase *usecase.LinkListUseCase,
	errorHandler response.ErrorHandler,
	applicationLogger logger.Logger,
) *LinkHandler {
	return &LinkHandler{
		linkCreateUseCase: linkCreateUseCase,
		linkGetUseCase:    linkGetUseCase,
		linkListUseCase:   linkListUseCase,
		errorHandler:      errorHandler,
		applicationLogger: applicationLogger,
	}
}

func (h *LinkHandler) List(w http.ResponseWriter, r *http.Request) {
	const defaultPageSize = 20

	input := usecase.LinkListInput{Page: 1, PageSize: defaultPageSize}

	query, err := url.ParseQuery(r.URL.RawQuery)
	if err != nil {
		h.errorHandler.Error(w, errInvalidInput)
		return
	}

	for _, param := range []struct {
		name  string
		value *int
	}{{"page", &input.Page}, {"page_size", &input.PageSize}} {
		values, ok := query[param.name]
		if !ok {
			continue
		}

		if len(values) != 1 {
			h.errorHandler.Error(w, errInvalidInput)
			return
		}

		value, parseErr := strconv.Atoi(values[0])
		if parseErr != nil {
			h.errorHandler.Error(w, errInvalidInput)
			return
		}

		*param.value = value
	}

	output, err := h.linkListUseCase.Execute(r.Context(), input)
	if err != nil {
		if errors.Is(err, errs.ErrInvalidInput) {
			err = errInvalidInput
		}

		h.errorHandler.Error(w, err)

		return
	}

	if err = response.JSON(w, http.StatusOK, mapper.ToLinkListResponse(output), nil); err != nil {
		h.applicationLogger.Error("write link list response", logger.Error(err))
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
		h.applicationLogger.Error("write link retrieval response", logger.Error(err))
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
		h.applicationLogger.Error("write link creation response", logger.Error(err))
	}
}
