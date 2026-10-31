package validator_test

import (
	"strings"
	"testing"

	"github.com/cristiano-pacheco/go-shortner/internal/modules/shortener/errs"
	"github.com/cristiano-pacheco/go-shortner/internal/modules/shortener/validator"
	"github.com/stretchr/testify/suite"
)

type DestinationValidatorSuite struct {
	suite.Suite
	validator *validator.DestinationValidator
}

func TestDestinationValidatorSuite(t *testing.T) {
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
	}{
		{"empty", ""},
		{"missing scheme", "example.com/path"},
		{"unsupported scheme", "ftp://example.com"},
		{"missing host", "https:///path"},
		{"credentials", "https://user:password@example.com"},
		{"empty credentials", "https://@example.com"},
		{"space", "https://example.com/a b"},
		{"unicode whitespace", "https://example.com/a\u00a0b"},
		{"malformed escape", "https://example.com/%zz"},
		{"above maximum length", prefix + strings.Repeat("a", 4097-len(prefix))},
	}

	for _, tc := range cases {
		s.Run(tc.name, func() {
			s.ErrorIs(s.validator.Validate(tc.destination), errs.ErrInvalidInput)
		})
	}
}
