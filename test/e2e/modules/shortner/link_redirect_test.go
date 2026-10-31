//go:build e2e

package shortner_test

import (
	"github.com/stretchr/testify/suite"
	"testing"
)

import (
	"fmt"
	"time"
)

type RedirectLinkSuite struct {
	endpointSuite
}

func TestRedirectLinkSuite(t *testing.T) {
	suite.Run(t, new(RedirectLinkSuite))
}

func (s *RedirectLinkSuite) TestRedirect() {
	t := s.T()
	s.create("redirect-e2e")
	for _, method := range []string{"GET", "HEAD"} {
		h, b := s.http.expect(t, method, "/redirect-e2e", "", "", "", 302)
		s.Equal("https://example.com/path", h.Get("Location"))
		s.Equal("no-store", h.Get("Cache-Control"))
		if method == "HEAD" {
			s.Empty(b)
		}
		_, b = s.http.expect(t, method, "/REDIRECT-E2E", "", "", "", 404)
		if method == "HEAD" {
			s.Empty(b)
		}
	}
	expires := time.Now().UTC().Add(5 * time.Second)
	payload := fmt.Sprintf(`{"destination":"https://example.com/expiring","alias":"expires-e2e","expires_at":%q}`, expires.Format(time.RFC3339Nano))
	s.http.expect(t, "POST", "/api/v1/links", payload, apiKey, "application/json", 201)
	h, _ := s.http.expect(t, "GET", "/expires-e2e", "", "", "", 302)
	s.Equal("https://example.com/expiring", h.Get("Location"))
	if delay := time.Until(expires.Add(200 * time.Millisecond)); delay > 0 {
		time.Sleep(delay)
	}
	s.http.expect(t, "GET", "/expires-e2e", "", "", "", 404)
}

func (s *RedirectLinkSuite) TestRedirectDatabaseFixtures() {
	t := s.T()
	s.sql(`INSERT INTO links (code, destination, active, expires_at, deleted_at) VALUES
 ('FixtureCase', 'https://example.com/a?x=1&y=two', true, NULL, NULL),
 ('fixture-inactive', 'https://example.com/inactive', false, NULL, NULL),
 ('fixture-expired', 'https://example.com/expired', true, '2020-01-01T00:00:00Z', NULL),
 ('fixture-deleted', 'https://example.com/deleted', true, NULL, now())`)
	for _, method := range []string{"GET", "HEAD"} {
		headers, body := s.http.expect(t, method, "/FixtureCase", "", "", "", 302)
		s.Equal("https://example.com/a?x=1&y=two", headers.Get("Location"))
		s.Equal("no-store", headers.Get("Cache-Control"))
		if method == "HEAD" {
			s.Empty(body)
		}
	}
	var missingBody []byte
	for _, code := range []string{"fixture-missing", "fixturecase", "fixture-inactive", "fixture-expired", "fixture-deleted"} {
		headers, body := s.http.expect(t, "GET", "/"+code, "", "", "", 404)
		s.Empty(headers.Get("Location"))
		if missingBody == nil {
			missingBody = body
		} else {
			s.Equal(missingBody, body)
		}
		_, body = s.http.expect(t, "HEAD", "/"+code, "", "", "", 404)
		s.Empty(body)
	}
}
