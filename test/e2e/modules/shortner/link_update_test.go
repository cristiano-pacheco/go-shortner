//go:build e2e

package shortner_test

import (
	"github.com/stretchr/testify/suite"
	"testing"
)

import "fmt"

type UpdateLinkSuite struct {
	endpointSuite
}

func TestUpdateLinkSuite(t *testing.T) {
	suite.Run(t, new(UpdateLinkSuite))
}

func (s *UpdateLinkSuite) TestUpdateLink() {
	t := s.T()
	item := s.create("update-e2e")
	path := fmt.Sprintf("/api/v1/links/%d", item.ID)
	for _, key := range []string{"", "wrong"} {
		s.http.expect(t, "PATCH", path, `{"active":false}`, key, "application/json", 401)
	}
	for _, body := range []string{`{"code":"other"}`, `{"destination":"ftp://example.com"}`, `{"active":null}`, `{"destination":null}`, `{"expires_at":"2020-01-01T00:00:00Z"}`, `{"active":false`} {
		s.http.expect(t, "PATCH", path, body, apiKey, "application/json", 400)
	}
	for _, id := range []string{"abc", "0", "-1", "9223372036854775808"} {
		s.http.expect(t, "PATCH", "/api/v1/links/"+id, `{"active":false}`, apiKey, "application/json", 400)
	}
	s.http.expect(t, "PATCH", "/api/v1/links/999999999", `{"active":false}`, apiKey, "application/json", 404)
	s.http.expect(t, "GET", "/update-e2e", "", "", "", 302)
	_, b := s.http.expect(t, "PATCH", path, `{"destination":"https://new.example/path","active":false,"expires_at":null}`, apiKey, "application/json", 200)
	updated := decodeLink(t, b)
	s.Equal("https://new.example/path", updated.Destination)
	s.False(updated.Active)
	s.Nil(updated.ExpiresAt)
	s.Equal(item.Code, updated.Code)
	s.http.expect(t, "GET", "/update-e2e", "", "", "", 404)
	s.http.expect(t, "PATCH", path, `{"active":true}`, apiKey, "application/json", 200)
	h, _ := s.http.expect(t, "GET", "/update-e2e", "", "", "", 302)
	s.Equal("https://new.example/path", h.Get("Location"))
}
