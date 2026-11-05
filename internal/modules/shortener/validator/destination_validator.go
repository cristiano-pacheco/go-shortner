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
	if destination == "" {
		return errs.ErrDestinationRequired
	}
	if len(destination) > 4096 {
		return errs.ErrDestinationTooLong
	}
	if strings.IndexFunc(destination, unicode.IsSpace) >= 0 {
		return errs.ErrDestinationWhitespace
	}
	u, err := url.Parse(destination)
	if err != nil || u == nil {
		return errs.ErrInvalidDestination
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return errs.ErrDestinationScheme
	}
	if u.Hostname() == "" {
		return errs.ErrDestinationHost
	}
	if u.User != nil {
		return errs.ErrDestinationCredentials
	}

	return nil
}
