package errs

import (
	"context"
	"errors"

	playgroundvalidator "github.com/go-playground/validator/v10"
)

var (
	ErrLinkNotFound    = errors.New("link not found")
	ErrCodeConflict    = errors.New("code already reserved")
	ErrInvalidInput    = errors.New("invalid input")
	ErrInvalidSnapshot = errors.New("invalid redirect snapshot")
)

// MetricCategory maps errors to bounded, stable operational categories.
func MetricCategory(err error) string {
	var validationErrors playgroundvalidator.ValidationErrors
	switch {
	case err == nil:
		return ""
	case errors.Is(err, ErrInvalidInput), errors.As(err, &validationErrors):
		return "invalid_argument"
	case errors.Is(err, ErrCodeConflict):
		return "conflict"
	case errors.Is(err, ErrLinkNotFound):
		return "not_found"
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		return "canceled"
	default:
		return "internal"
	}
}
