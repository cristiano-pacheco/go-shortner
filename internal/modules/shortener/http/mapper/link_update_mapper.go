package mapper

import (
	"encoding/json"
	"errors"
	"time"

	"github.com/cristiano-pacheco/go-shortner/internal/modules/shortener/http/dto"
	"github.com/cristiano-pacheco/go-shortner/internal/modules/shortener/usecase"
)

func ToLinkUpdateInput(id int64, request dto.LinkUpdateRequest) (usecase.LinkUpdateInput, error) {
	input := usecase.LinkUpdateInput{ID: id}
	if request.Destination != nil {
		if err := json.Unmarshal(request.Destination, &input.Destination); err != nil || input.Destination == nil {
			return input, errors.New("invalid destination")
		}
	}

	if request.Active != nil {
		if err := json.Unmarshal(request.Active, &input.Active); err != nil || input.Active == nil {
			return input, errors.New("invalid active state")
		}
	}

	if request.ExpiresAt != nil {
		input.SetExpiresAt = true

		var expiresAt *time.Time
		if err := json.Unmarshal(request.ExpiresAt, &expiresAt); err != nil {
			return input, err
		}

		input.ExpiresAt = expiresAt
	}

	return input, nil
}
