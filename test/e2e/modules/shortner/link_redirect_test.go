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

//nolint:paralleltest // The suite restarts its Compose dependencies.
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
	s.sql(
		`INSERT INTO links (code, destination, expires_at) VALUES ('expired', 'https://example.com/', '2020-01-01T00:00:00Z')`,
	)
	s.assertNotFound("expired")
}

func (s *RedirectLinkSuite) TestDeletedLinkIsNotFound() {
	s.sql(`INSERT INTO links (code, destination, deleted_at) VALUES ('deleted', 'https://example.com/', now())`)
	s.assertNotFound("deleted")
}

func (s *RedirectLinkSuite) TestFutureExpirationAllowsRedirect() {
	s.sql(
		`INSERT INTO links (code, destination, expires_at) VALUES ('future', 'https://example.com/future', '2099-01-01T00:00:00Z')`,
	)
	headers, _ := s.http.expect(s.T(), http.MethodGet, "/future", "", "", "", http.StatusFound)
	s.Equal("https://example.com/future", headers.Get("Location"))
}

func (s *RedirectLinkSuite) TestWarmCacheStopsRedirectingAfterExpiration() {
	expires := time.Now().UTC().Add(4 * time.Second)
	payload := fmt.Sprintf(
		`{"destination":"https://example.com/expiring","alias":"expiring-cache","expires_at":%q}`,
		expires.Format(time.RFC3339Nano),
	)
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
	s.sql(
		`INSERT INTO links (code, destination, expires_at) VALUES ('expired-cache', 'https://example.com/expired', '2020-01-01T00:00:00Z')`,
	)

	key := "shortener:redirect:v1:expired-cache"
	// Retain an expired snapshot with a future freshness deadline to exercise the cache expiration guard.
	value := fmt.Sprintf(
		`{"destination":"https://example.com/expired","active":true,"expires_at":"2020-01-01T00:00:00Z","fresh_until":%q}`,
		time.Now().UTC().Add(time.Hour).Format(time.RFC3339Nano),
	)
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
	s.http.expect(
		s.T(),
		http.MethodPatch,
		path,
		`{"destination":"https://new.example/path"}`,
		apiKey,
		"application/json",
		http.StatusOK,
	)
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

func (s *RedirectLinkSuite) TestDependencyFailureAndRestart() {
	s.create("dependency-fallback")
	out, err := s.http.run("stop", "redis")

	s.Require().NoError(err, "%s", out)
	defer func() {
		_, _ = s.http.run("up", "-d", "--wait", "redis")
	}()

	headers, _ := s.http.expect(s.T(), http.MethodGet, "/dependency-fallback", "", "", "", http.StatusFound)
	s.Equal("https://example.com/path", headers.Get("Location"))
	out, err = s.http.run("run", "--rm", "--no-deps", "app", "server")
	s.Require().Error(err, "server should reject unavailable Redis: %s", out)
	s.Contains(string(out), "cannot connect to Redis")
	out, err = s.http.run("up", "-d", "--wait", "redis")
	s.Require().NoError(err, "%s", out)
	out, err = s.http.run("stop", "postgres")

	s.Require().NoError(err, "%s", out)
	defer func() {
		_, _ = s.http.run("up", "-d", "--wait", "postgres")
	}()

	out, err = s.http.run("run", "--rm", "--no-deps", "app", "server")
	s.Require().Error(err, "server should reject unavailable PostgreSQL: %s", out)
	s.Contains(string(out), "cannot connect to PostgreSQL")
	out, err = s.http.run("up", "-d", "--wait", "postgres")
	s.Require().NoError(err, "%s", out)
	out, err = s.http.run("restart", "app")
	s.Require().NoError(err, "%s", out)
	out, err = s.http.run("port", "app", "8080")
	s.Require().NoError(err, "%s", out)
	s.http.base = "http://" + strings.TrimSpace(string(out))

	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		response, requestErr := s.http.client.Get(s.http.base + "/health")
		if requestErr == nil {
			response.Body.Close()

			if response.StatusCode == http.StatusOK {
				s.http.expect(s.T(), http.MethodGet, "/dependency-fallback", "", "", "", http.StatusFound)
				return
			}
		}

		time.Sleep(200 * time.Millisecond)
	}

	logs, _ := s.http.run("logs", "app")
	s.FailNow("application failed to restart", "%s", logs)
}

//nolint:gocognit // The test coordinates a blocked request, shutdown, and restart.
func (s *RedirectLinkSuite) TestShutdownDrainsInFlightDatabaseOperation() {
	item := s.create("drain-inflight")
	s.http.expect(s.T(), http.MethodGet, "/drain-inflight", "", "", "", http.StatusFound)

	cacheKey := "shortener:redirect:v1:drain-inflight"
	s.Require().Equal("1", s.redis("EXISTS", cacheKey))

	lockDone := make(chan error, 1)

	go func() {
		out, err := s.http.run(
			"exec",
			"-T",
			"postgres",
			"psql",
			"-X",
			"-v",
			"ON_ERROR_STOP=1",
			"-U",
			"shortener",
			"-d",
			"shortener",
			"-c",
			"BEGIN; SELECT id FROM links WHERE code = 'drain-inflight' FOR UPDATE; SELECT pg_sleep(3); COMMIT",
		)
		if err != nil {
			lockDone <- fmt.Errorf("lock fixture: %w: %s", err, out)
			return
		}

		lockDone <- nil
	}()

	query := func(statement string) string {
		out, err := s.http.run(
			"exec",
			"-T",
			"postgres",
			"psql",
			"-X",
			"-A",
			"-t",
			"-U",
			"shortener",
			"-d",
			"shortener",
			"-c",
			statement,
		)
		s.Require().NoError(err, "%s", out)

		return strings.TrimSpace(string(out))
	}

	s.Require().Eventually(func() bool {
		return query(
			"SELECT count(*) FROM pg_stat_activity WHERE wait_event = 'PgSleep' AND query LIKE '%drain-inflight%'",
		) == "1"
	}, 5*time.Second, 100*time.Millisecond)

	type result struct {
		status int
		err    error
	}

	requestDone := make(chan result, 1)

	go func() {
		client := &http.Client{Timeout: 20 * time.Second}

		req, err := http.NewRequest(
			http.MethodPatch,
			fmt.Sprintf("%s/api/v1/links/%d", s.http.base, item.ID),
			strings.NewReader(`{"destination":"https://example.com/drained"}`),
		)
		if err != nil {
			requestDone <- result{err: err}
			return
		}

		req.Header.Set("X-Api-Key", apiKey)
		req.Header.Set("Content-Type", "application/json")

		resp, err := client.Do(req)
		if err != nil {
			requestDone <- result{err: err}
			return
		}

		resp.Body.Close()

		requestDone <- result{status: resp.StatusCode}
	}()

	s.Require().Eventually(func() bool {
		return query(
			"SELECT count(*) FROM pg_stat_activity WHERE wait_event_type = 'Lock' AND query LIKE 'UPDATE %links%'",
		) == "1"
	}, 5*time.Second, 100*time.Millisecond)

	stopDone := make(chan error, 1)

	go func() {
		out, err := s.http.run("stop", "--timeout", "15", "app")
		if err != nil {
			stopDone <- fmt.Errorf("stop app: %w: %s", err, out)
			return
		}

		stopDone <- nil
	}()

	s.Require().Eventually(func() bool {
		response, requestErr := s.http.client.Get(s.http.base + "/health")
		if requestErr != nil {
			return true
		}

		response.Body.Close()

		return false
	}, 2*time.Second, 50*time.Millisecond, "HTTP listener did not stop accepting requests")

	select {
	case got := <-requestDone:
		s.FailNow("request completed before lock release", "%+v", got)
	case err := <-stopDone:
		s.FailNow("shutdown completed before request", "%v", err)
	case <-time.After(500 * time.Millisecond):
	}

	s.Require().NoError(<-lockDone)

	completed := <-requestDone
	s.Require().NoError(completed.err)
	s.Require().Equal(http.StatusOK, completed.status)
	s.Require().NoError(<-stopDone)
	s.Equal("0", s.redis("EXISTS", cacheKey))
	s.Equal("https://example.com/drained", query("SELECT destination FROM links WHERE code = 'drain-inflight'"))
	out, err := s.http.run("up", "-d", "--no-deps", "app")
	s.Require().NoError(err, "%s", out)
	out, err = s.http.run("port", "app", "8080")
	s.Require().NoError(err, "%s", out)
	s.http.base = "http://" + strings.TrimSpace(string(out))
	s.Require().Eventually(func() bool {
		response, requestErr := s.http.client.Get(s.http.base + "/health")
		if requestErr != nil {
			return false
		}

		response.Body.Close()

		return response.StatusCode == http.StatusOK
	}, 30*time.Second, 200*time.Millisecond)
}
