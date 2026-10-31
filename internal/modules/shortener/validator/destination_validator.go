package validator

import (
	"net/url"
	"strings"
	"unicode"

	"github.com/cristiano-pacheco/go-shortner/internal/modules/shortener/errs"
	"github.com/cristiano-pacheco/go-shortner/internal/modules/shortener/port"
)

type DestinationValidator struct{}

var _ port.DestinationValidator = (*DestinationValidator)(nil)

func NewDestinationValidator() *DestinationValidator {
	return &DestinationValidator{}
}

func (v *DestinationValidator) Validate(destination string) error {
	u, err := url.Parse(destination)
	if len(destination) > 4096 || strings.IndexFunc(destination, unicode.IsSpace) >= 0 || err != nil || u == nil ||
		(u.Scheme != "http" && u.Scheme != "https") ||
		u.Hostname() == "" ||
		u.User != nil {
		return errs.ErrInvalidInput
	}

	return nil
}
