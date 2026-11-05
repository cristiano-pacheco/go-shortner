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
		if err := json.Unmarshal(request.Active, &input.Active); err != nil {
			return input, fmt.Errorf("%w: %w", errs.ErrInvalidActive, err)
		}
		if input.Active == nil {
			return input, errs.ErrInvalidActive
		}
	}

	if request.ExpiresAt != nil {
		input.SetExpiresAt = true

		var expiresAt *time.Time
		if err := json.Unmarshal(request.ExpiresAt, &expiresAt); err != nil {
			return input, fmt.Errorf("%w: %w", errs.ErrInvalidExpiration, err)
		}

		input.ExpiresAt = expiresAt
	}

	return input, nil
}
