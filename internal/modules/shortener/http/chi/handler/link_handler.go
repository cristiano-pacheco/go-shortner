package handler

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"

	brickserrs "github.com/cristiano-pacheco/bricks/pkg/errs"
	"github.com/cristiano-pacheco/bricks/pkg/http/response"
	"github.com/cristiano-pacheco/go-shortner/internal/modules/shortener/errs"
	"github.com/cristiano-pacheco/go-shortner/internal/modules/shortener/http/dto"
	"github.com/cristiano-pacheco/go-shortner/internal/modules/shortener/usecase"
)

var (
	invalidInput = brickserrs.New("invalid_argument", "Invalid request", http.StatusBadRequest, nil)
	codeConflict = brickserrs.New("code_conflict", "Code is already reserved", http.StatusConflict, nil)
)

type LinkHandler struct {
	create *usecase.LinkCreateUseCase
	errors response.ErrorHandler
}

func NewLinkHandler(create *usecase.LinkCreateUseCase, errors response.ErrorHandler) *LinkHandler {
	return &LinkHandler{create: create, errors: errors}
}

func (h *LinkHandler) Create(w http.ResponseWriter, r *http.Request) {
	var request dto.LinkCreateRequest
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&request); err != nil {
		h.errors.Error(w, invalidInput)
		return
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		h.errors.Error(w, invalidInput)
		return
	}
	output, err := h.create.Execute(r.Context(), usecase.LinkCreateInput{Destination: request.Destination, Alias: request.Alias, ExpiresAt: request.ExpiresAt})
	if err != nil {
		switch {
		case errors.Is(err, errs.ErrInvalidInput):
			err = invalidInput
		case errors.Is(err, errs.ErrCodeConflict):
			err = codeConflict
		}
		h.errors.Error(w, err)
		return
	}
	_ = response.JSON(w, http.StatusCreated, dto.LinkResponse{ID: output.ID, Code: output.Code, Destination: output.Destination, ShortURL: output.ShortURL, Active: output.Active, ExpiresAt: output.ExpiresAt, CreatedAt: output.CreatedAt, UpdatedAt: output.UpdatedAt}, nil)
}
