package validator_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/suite"

	"github.com/cristiano-pacheco/go-shortner/internal/modules/shortener/errs"
	"github.com/cristiano-pacheco/go-shortner/internal/modules/shortener/validator"
)

type DestinationValidatorSuite struct {
	suite.Suite

	validator *validator.DestinationValidator
}

func TestDestinationValidatorSuite(t *testing.T) {
	t.Parallel()

	suite.Run(t, new(DestinationValidatorSuite))
}

func (s *DestinationValidatorSuite) SetupTest() {
	s.validator = validator.NewDestinationValidator()
}

func (s *DestinationValidatorSuite) TestValidateAcceptsValidDestinations() {
	prefix := "https://example.com/"
	cases := []struct {
		name        string
		destination string
	}{
		{"http", "http://example.com"},
		{"https with path query and fragment", "https://example.com:8443/path?q=value#section"},
		{"ip address", "http://127.0.0.1:8080/path"},
		{"encoded space", "https://example.com/a%20b"},
		{"maximum length", prefix + strings.Repeat("a", 4096-len(prefix))},
	}

	for _, tc := range cases {
		s.Run(tc.name, func() {
			s.NoError(s.validator.Validate(tc.destination))
		})
	}
}

func (s *DestinationValidatorSuite) TestValidateRejectsInvalidDestinations() {
	prefix := "https://example.com/"
	cases := []struct {
		name        string
		destination string
		want        error
	}{
		{"empty", "", errs.ErrDestinationRequired},
		{"missing scheme", "example.com/path", errs.ErrDestinationScheme},
		{"unsupported scheme", "ftp://example.com", errs.ErrDestinationScheme},
		{"missing host", "https:///path", errs.ErrDestinationHost},
		{"credentials", "https://user:password@example.com", errs.ErrDestinationCredentials},
		{"empty credentials", "https://@example.com", errs.ErrDestinationCredentials},
		{"space", "https://example.com/a b", errs.ErrDestinationWhitespace},
		{"unicode whitespace", "https://example.com/a\u00a0b", errs.ErrDestinationWhitespace},
		{"malformed escape", "https://example.com/%zz", errs.ErrInvalidDestination},
		{"above maximum length", prefix + strings.Repeat("a", 4097-len(prefix)), errs.ErrDestinationTooLong},
	}

	for _, tc := range cases {
		s.Run(tc.name, func() {
			s.ErrorIs(s.validator.Validate(tc.destination), tc.want)
		})
	}
}
