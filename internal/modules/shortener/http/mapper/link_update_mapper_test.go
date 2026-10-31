package mapper_test

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/cristiano-pacheco/go-shortner/internal/modules/shortener/http/dto"
	"github.com/cristiano-pacheco/go-shortner/internal/modules/shortener/http/mapper"
	"github.com/cristiano-pacheco/go-shortner/internal/modules/shortener/usecase"
	"github.com/stretchr/testify/suite"
)

type LinkUpdateMapperSuite struct {
	suite.Suite
}

func TestLinkUpdateMapperSuite(t *testing.T) {
	suite.Run(t, new(LinkUpdateMapperSuite))
}

func (s *LinkUpdateMapperSuite) TestOmittedFieldsRemainUnset() {
	input, err := mapper.ToLinkUpdateInput(42, dto.LinkUpdateRequest{})

	s.Require().NoError(err)
	s.Equal(usecase.LinkUpdateInput{ID: 42}, input)
}

func (s *LinkUpdateMapperSuite) TestMapsExplicitValues() {
	input, err := mapper.ToLinkUpdateInput(42, dto.LinkUpdateRequest{
		Destination: json.RawMessage(`"https://example.com"`),
		Active:      json.RawMessage(`false`),
		ExpiresAt:   json.RawMessage(`"2026-10-29T12:30:00+02:00"`),
	})

	s.Require().NoError(err)
	s.Equal(int64(42), input.ID)
	s.Require().NotNil(input.Destination)
	s.Equal("https://example.com", *input.Destination)
	s.Require().NotNil(input.Active)
	s.False(*input.Active)
	s.True(input.SetExpiresAt)
	s.Require().NotNil(input.ExpiresAt)
	s.Equal("2026-10-29T12:30:00+02:00", input.ExpiresAt.Format(time.RFC3339))
}

func (s *LinkUpdateMapperSuite) TestNullExpirationClearsValue() {
	input, err := mapper.ToLinkUpdateInput(42, dto.LinkUpdateRequest{ExpiresAt: json.RawMessage(`null`)})

	s.Require().NoError(err)
	s.True(input.SetExpiresAt)
	s.Nil(input.ExpiresAt)
}

func (s *LinkUpdateMapperSuite) TestRejectsInvalidFields() {
	cases := []struct {
		name    string
		request dto.LinkUpdateRequest
	}{
		{"null destination", dto.LinkUpdateRequest{Destination: json.RawMessage(`null`)}},
		{"non-string destination", dto.LinkUpdateRequest{Destination: json.RawMessage(`123`)}},
		{"malformed destination", dto.LinkUpdateRequest{Destination: json.RawMessage(`"unfinished`)}},
		{"null active", dto.LinkUpdateRequest{Active: json.RawMessage(`null`)}},
		{"non-boolean active", dto.LinkUpdateRequest{Active: json.RawMessage(`"false"`)}},
		{"malformed active", dto.LinkUpdateRequest{Active: json.RawMessage(`tru`)}},
		{"invalid expiration", dto.LinkUpdateRequest{ExpiresAt: json.RawMessage(`"not-a-date"`)}},
		{"non-string expiration", dto.LinkUpdateRequest{ExpiresAt: json.RawMessage(`123`)}},
		{"malformed expiration", dto.LinkUpdateRequest{ExpiresAt: json.RawMessage(`"unfinished`)}},
	}

	for _, tc := range cases {
		s.Run(tc.name, func() {
			_, err := mapper.ToLinkUpdateInput(42, tc.request)
			s.Error(err)
		})
	}
}
