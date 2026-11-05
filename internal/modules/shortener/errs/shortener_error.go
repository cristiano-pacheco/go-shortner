package errs

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	brickserrs "github.com/cristiano-pacheco/bricks/pkg/errs"
	playgroundvalidator "github.com/go-playground/validator/v10"
)

var (
	ErrLinkNotFound    = brickserrs.New("SHORTENER_01", "link not found", http.StatusNotFound, nil)
	ErrCodeConflict    = brickserrs.New("SHORTENER_02", "code already reserved", http.StatusConflict, nil)
	ErrInvalidSnapshot = brickserrs.New(
		"SHORTENER_04",
		"invalid redirect snapshot",
		http.StatusInternalServerError,
		nil,
	)
	ErrCodeGenerationExhausted = brickserrs.New(
		"SHORTENER_05",
		"automatic code generation exhausted its retry limit",
		http.StatusInternalServerError,
		nil,
	)
	ErrInvalidLinkID = brickserrs.New(
		"SHORTENER_30",
		"link ID must be a positive integer",
		http.StatusBadRequest,
		nil,
	)
	ErrDestinationRequired = brickserrs.New("SHORTENER_31", "destination is required", http.StatusBadRequest, nil)
	ErrInvalidDestination  = brickserrs.New(
		"SHORTENER_32",
		"destination must be a valid URL string",
		http.StatusBadRequest,
		nil,
	)
	ErrDestinationScheme = brickserrs.New(
		"SHORTENER_33",
		"destination scheme must be HTTP or HTTPS",
		http.StatusBadRequest,
		nil,
	)
	ErrDestinationHost = brickserrs.New(
		"SHORTENER_34",
		"destination must include a host",
		http.StatusBadRequest,
		nil,
	)
	ErrDestinationCredentials = brickserrs.New(
		"SHORTENER_35",
		"destination must not include credentials",
		http.StatusBadRequest,
		nil,
	)
	ErrDestinationWhitespace = brickserrs.New(
		"SHORTENER_36",
		"destination must not contain whitespace",
		http.StatusBadRequest,
		nil,
	)
	ErrDestinationTooLong = brickserrs.New(
		"SHORTENER_37",
		"destination must not exceed 4096 characters",
		http.StatusBadRequest,
		nil,
	)
	ErrAliasLength = brickserrs.New(
		"SHORTENER_38",
		"alias must be between 3 and 32 characters",
		http.StatusBadRequest,
		nil,
	)
	ErrAliasReserved   = brickserrs.New("SHORTENER_39", "alias is reserved", http.StatusBadRequest, nil)
	ErrAliasCharacters = brickserrs.New(
		"SHORTENER_40",
		"alias may only contain letters, numbers, hyphens, and underscores",
		http.StatusBadRequest,
		nil,
	)
	ErrExpirationInPast = brickserrs.New(
		"SHORTENER_41",
		"expiration must be in the future",
		http.StatusBadRequest,
		nil,
	)
	ErrInvalidPage = brickserrs.New(
		"SHORTENER_42",
		"page must be a positive integer",
		http.StatusBadRequest,
		nil,
	)
	ErrInvalidPageSize = brickserrs.New(
		"SHORTENER_43",
		"page size must be an integer between 1 and 100",
		http.StatusBadRequest,
		nil,
	)
	ErrPaginationOverflow = brickserrs.New(
		"SHORTENER_44",
		"pagination offset exceeds the supported range",
		http.StatusBadRequest,
		nil,
	)
	ErrInvalidQuery = brickserrs.New(
		"SHORTENER_45",
		"query string contains invalid encoding",
		http.StatusBadRequest,
		nil,
	)
	ErrMissingRequest = brickserrs.New(
		"SHORTENER_46",
		"request body must be a JSON object",
		http.StatusBadRequest,
		nil,
	)
	ErrInvalidActive     = brickserrs.New("SHORTENER_47", "active must be a boolean", http.StatusBadRequest, nil)
	ErrInvalidExpiration = brickserrs.New(
		"SHORTENER_48",
		"expiration must be an RFC3339 timestamp or null",
		http.StatusBadRequest,
		nil,
	)
	ErrCodeRequired = brickserrs.New("SHORTENER_49", "link code is required", http.StatusBadRequest, nil)
	ErrPageRepeated = brickserrs.New(
		"SHORTENER_50",
		"page must not be specified more than once",
		http.StatusBadRequest,
		nil,
	)
	ErrPageSizeRepeated = brickserrs.New(
		"SHORTENER_51",
		"page size must not be specified more than once",
		http.StatusBadRequest,
		nil,
	)
	ErrDestinationType = brickserrs.New(
		"SHORTENER_60",
		"destination must be a string",
		http.StatusBadRequest,
		nil,
	)
	ErrAliasType = brickserrs.New(
		"SHORTENER_61",
		"alias must be a string or null",
		http.StatusBadRequest,
		nil,
	)
)

func ValidationError(err error) error {
	var validationErrors playgroundvalidator.ValidationErrors
	if !errors.As(err, &validationErrors) || len(validationErrors) == 0 {
		return err
	}

	var mapped error

	switch validationErrors[0].Field() {
	case "ID":
		mapped = ErrInvalidLinkID
	case "Destination":
		mapped = ErrDestinationRequired
	case "Alias":
		mapped = ErrAliasLength
	case "ExpiresAt":
		mapped = ErrExpirationInPast
	case "Page":
		mapped = ErrInvalidPage
	case "PageSize":
		mapped = ErrInvalidPageSize
	case "Code":
		mapped = ErrCodeRequired
	default:
		return err
	}

	return fmt.Errorf("%w: %w", mapped, err)
}

func MetricCategory(err error) string {
	var (
		validationErrors playgroundvalidator.ValidationErrors
		codedError       *brickserrs.Error
	)
	switch {
	case err == nil:
		return ""
	case errors.As(err, &validationErrors):
		return "invalid_argument"
	case errors.Is(err, ErrCodeConflict):
		return "conflict"
	case errors.Is(err, ErrLinkNotFound):
		return "not_found"
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		return "canceled"
	case errors.As(err, &codedError) && codedError.Status == http.StatusBadRequest:
		return "invalid_argument"
	default:
		return "internal"
	}
}
