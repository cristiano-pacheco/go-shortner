//go:build e2e

package shortner_test

import (
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/suite"
)

type RedirectLinkSuite struct {
	endpointSuite
}

func TestRedirectLinkSuite(t *testing.T) {
	suite.Run(t, new(RedirectLinkSuite))
}

func (s *RedirectLinkSuite) assertNotFound(code string) {
	s.T().Helper()
	headers, _ := s.http.expect(s.T(), http.MethodGet, "/"+code, "", "", "", http.StatusNotFound)
	s.Empty(headers.Get("Location"))
}

func (s *RedirectLinkSuite) redis(args ...string) string {
	s.T().Helper()
	out, err := s.http.run(append([]string{"exec", "-T", "redis", "redis-cli", "--raw"}, args...)...)
	s.Require().NoError(err, "redis: %s", out)
	return strings.TrimSpace(string(out))
}

func (s *RedirectLinkSuite) TestGetRedirectPreservesDestination() {
	s.sql(`INSERT INTO links (code, destination) VALUES ('FixtureCase', 'https://example.com/a?x=1&y=two')`)
	headers, _ := s.http.expect(s.T(), http.MethodGet, "/FixtureCase", "", "", "", http.StatusFound)
	s.Equal("https://example.com/a?x=1&y=two", headers.Get("Location"))
	s.Equal("no-store", headers.Get("Cache-Control"))
}

func (s *RedirectLinkSuite) TestHeadRedirectHasNoBody() {
	s.sql(`INSERT INTO links (code, destination) VALUES ('head-link', 'https://example.com/head')`)
	headers, body := s.http.expect(s.T(), http.MethodHead, "/head-link", "", "", "", http.StatusFound)
	s.Equal("https://example.com/head", headers.Get("Location"))
	s.Equal("no-store", headers.Get("Cache-Control"))
	s.Empty(body)
}

func (s *RedirectLinkSuite) TestUnknownCodeIsNotFound() {
	s.assertNotFound("not-found")
}

func (s *RedirectLinkSuite) TestHeadUnknownCodeHasNoBody() {
	headers, body := s.http.expect(s.T(), http.MethodHead, "/not-found", "", "", "", http.StatusNotFound)
	s.Empty(headers.Get("Location"))
	s.Empty(body)
}

func (s *RedirectLinkSuite) TestCodeIsCaseSensitive() {
	s.sql(`INSERT INTO links (code, destination) VALUES ('FixtureCase', 'https://example.com/')`)
	s.assertNotFound("fixturecase")
	s.http.expect(s.T(), http.MethodGet, "/FixtureCase", "", "", "", http.StatusFound)
}

func (s *RedirectLinkSuite) TestInactiveLinkIsNotFound() {
	s.sql(`INSERT INTO links (code, destination, active) VALUES ('inactive', 'https://example.com/', false)`)
	s.assertNotFound("inactive")
}

func (s *RedirectLinkSuite) TestExpiredLinkIsNotFound() {
	s.sql(`INSERT INTO links (code, destination, expires_at) VALUES ('expired', 'https://example.com/', '2020-01-01T00:00:00Z')`)
	s.assertNotFound("expired")
}

func (s *RedirectLinkSuite) TestDeletedLinkIsNotFound() {
	s.sql(`INSERT INTO links (code, destination, deleted_at) VALUES ('deleted', 'https://example.com/', now())`)
	s.assertNotFound("deleted")
}

func (s *RedirectLinkSuite) TestFutureExpirationAllowsRedirect() {
	s.sql(`INSERT INTO links (code, destination, expires_at) VALUES ('future', 'https://example.com/future', '2099-01-01T00:00:00Z')`)
	headers, _ := s.http.expect(s.T(), http.MethodGet, "/future", "", "", "", http.StatusFound)
	s.Equal("https://example.com/future", headers.Get("Location"))
}

func (s *RedirectLinkSuite) TestWarmCacheStopsRedirectingAfterExpiration() {
	expires := time.Now().UTC().Add(15 * time.Second)
	payload := fmt.Sprintf(`{"destination":"https://example.com/expiring","alias":"expiring-cache","expires_at":%q}`, expires.Format(time.RFC3339Nano))
	s.http.expect(s.T(), http.MethodPost, "/api/v1/links", payload, apiKey, "application/json", http.StatusCreated)

	headers, _ := s.http.expect(s.T(), http.MethodGet, "/expiring-cache", "", "", "", http.StatusFound)
	s.Equal("https://example.com/expiring", headers.Get("Location"))
	s.NotEmpty(s.redis("GET", "shortener:redirect:v1:expiring-cache"))

	if delay := time.Until(expires.Add(200 * time.Millisecond)); delay > 0 {
		time.Sleep(delay)
	}
	s.assertNotFound("expiring-cache")
}

func (s *RedirectLinkSuite) TestExpiredCachedSnapshotDoesNotRedirect() {
	s.sql(`INSERT INTO links (code, destination, expires_at) VALUES ('expired-cache', 'https://example.com/expired', '2020-01-01T00:00:00Z')`)
	key := "shortener:redirect:v1:expired-cache"
	// Retain an expired snapshot with a future freshness deadline to exercise the cache expiration guard.
	value := fmt.Sprintf(`{"destination":"https://example.com/expired","active":true,"expires_at":"2020-01-01T00:00:00Z","fresh_until":%q}`, time.Now().UTC().Add(time.Hour).Format(time.RFC3339Nano))
	s.Equal("OK", s.redis("SET", key, value))
	s.assertNotFound("expired-cache")
	s.NotEmpty(s.redis("GET", key))
}

func (s *RedirectLinkSuite) TestUpdateInvalidatesWarmRedirectCache() {
	item := s.create("updated-cache")
	headers, _ := s.http.expect(s.T(), http.MethodGet, "/updated-cache", "", "", "", http.StatusFound)
	s.Equal("https://example.com/path", headers.Get("Location"))
	s.NotEmpty(s.redis("GET", "shortener:redirect:v1:updated-cache"))

	path := fmt.Sprintf("/api/v1/links/%d", item.ID)
	s.http.expect(s.T(), http.MethodPatch, path, `{"destination":"https://new.example/path"}`, apiKey, "application/json", http.StatusOK)
	headers, _ = s.http.expect(s.T(), http.MethodGet, "/updated-cache", "", "", "", http.StatusFound)
	s.Equal("https://new.example/path", headers.Get("Location"))
}

func (s *RedirectLinkSuite) TestDeactivationInvalidatesWarmRedirectCache() {
	item := s.create("inactive-cache")
	s.http.expect(s.T(), http.MethodGet, "/inactive-cache", "", "", "", http.StatusFound)
	s.NotEmpty(s.redis("GET", "shortener:redirect:v1:inactive-cache"))

	path := fmt.Sprintf("/api/v1/links/%d", item.ID)
	s.http.expect(s.T(), http.MethodPatch, path, `{"active":false}`, apiKey, "application/json", http.StatusOK)
	s.assertNotFound("inactive-cache")
}

func (s *RedirectLinkSuite) TestDeleteInvalidatesWarmRedirectCache() {
	item := s.create("deleted-cache")
	s.http.expect(s.T(), http.MethodGet, "/deleted-cache", "", "", "", http.StatusFound)
	s.NotEmpty(s.redis("GET", "shortener:redirect:v1:deleted-cache"))

	path := fmt.Sprintf("/api/v1/links/%d", item.ID)
	s.http.expect(s.T(), http.MethodDelete, path, "", apiKey, "", http.StatusNoContent)
	s.assertNotFound("deleted-cache")
}
