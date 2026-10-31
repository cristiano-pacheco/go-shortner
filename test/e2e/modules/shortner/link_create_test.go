//go:build e2e

package shortner_test

import (
	"github.com/stretchr/testify/suite"
	"testing"
)

import (
	"regexp"
	"strings"
)

type CreateLinkSuite struct {
	endpointSuite
}

func TestCreateLinkSuite(t *testing.T) {
	suite.Run(t, new(CreateLinkSuite))
}

func (s *CreateLinkSuite) TestCreateLink() {
	t := s.T()
	const path = "/api/v1/links"
	for _, key := range []string{"", "wrong"} {
		s.http.expect(t, "POST", path, `{"destination":"https://example.com"}`, key, "application/json", 401)
	}
	for _, tc := range []struct {
		name, body, media string
		status            int
	}{
		{"media", `{}`, "text/plain", 415}, {"unknown", `{"destination":"https://example.com","unknown":true}`, "application/json", 400},
		{"malformed", `{"destination":`, "application/json", 400}, {"trailing", `{} {}`, "application/json", 400},
		{"oversized", `{"destination":"https://example.com/` + strings.Repeat("a", 1<<20) + `"}`, "application/json", 413},
		{"missing", `{}`, "application/json", 400}, {"relative", `{"destination":"/relative"}`, "application/json", 400},
		{"scheme", `{"destination":"ftp://example.com"}`, "application/json", 400}, {"credentials", `{"destination":"https://u:p@example.com"}`, "application/json", 400},
		{"whitespace", `{"destination":"https://example.com/ "}`, "application/json", 400}, {"short alias", `{"destination":"https://example.com","alias":"ab"}`, "application/json", 400},
		{"long alias", `{"destination":"https://example.com","alias":"` + strings.Repeat("a", 33) + `"}`, "application/json", 400},
		{"invalid alias", `{"destination":"https://example.com","alias":"abc.def"}`, "application/json", 400},
		{"reserved", `{"destination":"https://example.com","alias":"health"}`, "application/json", 400},
		{"expired", `{"destination":"https://example.com","expires_at":"2020-01-01T00:00:00Z"}`, "application/json", 400},
	} {
		s.Run(tc.name, func() {
			s.http.expect(s.T(), "POST", path, tc.body, apiKey, tc.media, tc.status)
		})
	}
	_, b := s.http.expect(t, "POST", path, `{"destination":"https://example.com/path"}`, apiKey, "application/json", 201)
	item := decodeLink(t, b)
	s.True(regexp.MustCompile(`^[A-Za-z0-9]{8}$`).MatchString(item.Code))
	s.Positive(item.ID)
	s.Equal("https://example.com/path", item.Destination)
	s.Equal(publicBase+"/"+item.Code, item.ShortURL)
	s.True(item.Active)
	s.Nil(item.ExpiresAt)
	s.False(item.CreatedAt.IsZero())
	s.False(item.UpdatedAt.IsZero())
	_, b = s.http.expect(t, "POST", path, `{"destination":"https://example.com/a?x=1&y=two","alias":"Case_123","expires_at":"2099-01-01T00:00:00Z"}`, apiKey, "application/json", 201)
	item = decodeLink(t, b)
	s.Equal("Case_123", item.Code)
	s.Equal("https://example.com/a?x=1&y=two", item.Destination)
	s.Equal(publicBase+"/Case_123", item.ShortURL)
	s.NotNil(item.ExpiresAt)
	s.http.expect(t, "POST", path, `{"destination":"https://example.com","alias":"Case_123"}`, apiKey, "application/json", 409)
}
