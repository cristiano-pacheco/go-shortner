package mapper

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/cristiano-pacheco/go-shortner/internal/modules/shortener/errs"
	"github.com/cristiano-pacheco/go-shortner/internal/modules/shortener/http/dto"
	"github.com/cristiano-pacheco/go-shortner/internal/modules/shortener/usecase"
)

func ToLinkUpdateInput(id int64, request dto.LinkUpdateRequest) (usecase.LinkUpdateInput, error) {
	input := usecase.LinkUpdateInput{ID: id}
	if request.Destination != nil {
		if err := json.Unmarshal(request.Destination, &input.Destination); err != nil {
			return input, fmt.Errorf("%w: %w", errs.ErrInvalidDestination, err)
		}

		if input.Destination == nil {
			return input, errs.ErrInvalidDestination
		}
	}

	if request.Active != nil {
		if err := toActive(request.Active, &input.Active); err != nil {
			return input, err
		}
	}

	if request.ExpiresAt != nil {
		input.SetExpiresAt = true

		expiresAt, err := toExpiration(request.ExpiresAt)
		if err != nil {
			return input, err
		}

		input.ExpiresAt = expiresAt
	}

	return input, nil
}

func toActive(raw json.RawMessage, active **bool) error {
	if err := json.Unmarshal(raw, active); err != nil {
		return fmt.Errorf("%w: %w", errs.ErrInvalidActive, err)
	}

	if *active == nil {
		return errs.ErrInvalidActive
	}

	return nil
}

func toExpiration(raw json.RawMessage) (*time.Time, error) {
	var expiresAt *time.Time
	if err := json.Unmarshal(raw, &expiresAt); err != nil {
		return nil, fmt.Errorf("%w: %w", errs.ErrInvalidExpiration, err)
	}

	return expiresAt, nil
}
