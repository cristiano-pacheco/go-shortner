package handler

import (
	"encoding/json"
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
	sharederrs "github.com/cristiano-pacheco/go-shortner/internal/shared/errs"
	"github.com/cristiano-pacheco/go-shortner/internal/shared/httpserver"
)

type LinkHandler struct {
	linkCreateUseCase *usecase.LinkCreateUseCase
	linkGetUseCase    *usecase.LinkGetUseCase
	linkListUseCase   *usecase.LinkListUseCase
	linkUpdateUseCase *usecase.LinkUpdateUseCase
	linkDeleteUseCase *usecase.LinkDeleteUseCase
	errorHandler      response.ErrorHandler
	applicationLogger logger.Logger
}

func NewLinkHandler(
	linkCreateUseCase *usecase.LinkCreateUseCase,
	linkGetUseCase *usecase.LinkGetUseCase,
	linkListUseCase *usecase.LinkListUseCase,
	linkUpdateUseCase *usecase.LinkUpdateUseCase,
	linkDeleteUseCase *usecase.LinkDeleteUseCase,
	errorHandler response.ErrorHandler,
	applicationLogger logger.Logger,
) *LinkHandler {
	return &LinkHandler{
		linkCreateUseCase: linkCreateUseCase,
		linkGetUseCase:    linkGetUseCase,
		linkListUseCase:   linkListUseCase,
		linkUpdateUseCase: linkUpdateUseCase,
		linkDeleteUseCase: linkDeleteUseCase,
		errorHandler:      errorHandler,
		applicationLogger: applicationLogger,
	}
}

func (h *LinkHandler) Delete(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		h.errorHandler.Error(w, errs.ErrInvalidLinkID)
		return
	}

	_, err = h.linkDeleteUseCase.Execute(r.Context(), usecase.LinkDeleteInput{ID: id})
	if err != nil {
		h.writeError(w, err)

		return
	}

	response.NoContent(w)
}

func (h *LinkHandler) Update(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil || id <= 0 {
		h.errorHandler.Error(w, errs.ErrInvalidLinkID)
		return
	}

	var request *dto.LinkUpdateRequest
	if err = httpserver.ReadJSON(r, &request); err != nil {
		h.writeError(w, err)
		return
	}

	if request == nil {
		h.errorHandler.Error(w, errs.ErrMissingRequest)
		return
	}

	input, err := mapper.ToLinkUpdateInput(id, *request)
	if err != nil {
		h.writeError(w, err)
		return
	}

	output, err := h.linkUpdateUseCase.Execute(r.Context(), input)
	if err != nil {
		h.writeError(w, err)

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
		h.applicationLogger.Error("write link update response", logger.Error(err))
	}
}

func (h *LinkHandler) List(w http.ResponseWriter, r *http.Request) {
	const defaultPageSize = 20

	input := usecase.LinkListInput{Page: 1, PageSize: defaultPageSize}

	query, err := url.ParseQuery(r.URL.RawQuery)
	if err != nil {
		h.errorHandler.Error(w, errs.ErrInvalidQuery)
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
			if param.name == "page" {
				h.errorHandler.Error(w, errs.ErrPageRepeated)
			} else {
				h.errorHandler.Error(w, errs.ErrPageSizeRepeated)
			}
			return
		}

		value, parseErr := strconv.Atoi(values[0])
		if parseErr != nil {
			if param.name == "page" {
				h.errorHandler.Error(w, errs.ErrInvalidPage)
			} else {
				h.errorHandler.Error(w, errs.ErrInvalidPageSize)
			}
			return
		}

		*param.value = value
	}

	output, err := h.linkListUseCase.Execute(r.Context(), input)
	if err != nil {
		h.writeError(w, err)

		return
	}

	if err = response.JSON(w, http.StatusOK, mapper.ToLinkListResponse(output), nil); err != nil {
		h.applicationLogger.Error("write link list response", logger.Error(err))
	}
}

func (h *LinkHandler) Get(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		h.errorHandler.Error(w, errs.ErrInvalidLinkID)
		return
	}

	output, err := h.linkGetUseCase.Execute(r.Context(), usecase.LinkGetInput{ID: id})
	if err != nil {
		h.writeError(w, err)

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
		var fieldError *json.UnmarshalTypeError
		switch {
		case errors.As(err, &fieldError) && fieldError.Field == "destination":
			err = errs.ErrDestinationType
		case fieldError != nil && fieldError.Field == "alias":
			err = errs.ErrAliasType
		case errors.Is(err, sharederrs.ErrInvalidJSONTime):
			err = errs.ErrInvalidExpiration
		}
		h.writeError(w, err)
		return
	}

	if request == nil {
		h.errorHandler.Error(w, errs.ErrMissingRequest)
		return
	}

	output, err := h.linkCreateUseCase.Execute(
		r.Context(),
		usecase.LinkCreateInput{Destination: request.Destination, Alias: request.Alias, ExpiresAt: request.ExpiresAt},
	)
	if err != nil {
		if errors.Is(err, errs.ErrCodeGenerationExhausted) {
			err = brickserrs.New(errs.ErrCodeGenerationExhausted.Code, "internal server error", http.StatusInternalServerError, nil)
		}
		h.writeError(w, err)

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

func (h *LinkHandler) writeError(w http.ResponseWriter, err error) {
	var codedError *brickserrs.Error
	if errors.As(err, &codedError) {
		h.errorHandler.Error(w, codedError)
		return
	}
	h.errorHandler.Error(w, err)
}
