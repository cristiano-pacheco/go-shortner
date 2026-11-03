//go:build e2e

package shortner_test

import (
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
)

type CacheMetricsSuite struct {
	endpointSuite
}

func (s *CacheMetricsSuite) redis(args ...string) string {
	s.T().Helper()
	out, err := s.http.run(append([]string{"exec", "-T", "redis", "redis-cli", "--raw"}, args...)...)
	s.Require().NoError(err, "redis: %s", out)

	return strings.TrimSpace(string(out))
}

//nolint:paralleltest // The suite uses one isolated Compose stack and resets its dependencies.
func TestCacheMetricsSuite(t *testing.T) {
	suite.Run(t, new(CacheMetricsSuite))
}

func (s *CacheMetricsSuite) assertOperation(before, after httpMetricFamilies, operation, result string) {
	s.T().Helper()
	labels := map[string]string{"operation": operation, "result": result}
	for _, name := range []string{"shortener_cache_operations_total", "shortener_cache_operation_duration_seconds"} {
		s.Equal(before.value(name, labels)+1, after.value(name, labels), name)
	}
}

func (s *CacheMetricsSuite) assertDecision(before, after httpMetricFamilies, decision string) {
	s.T().Helper()
	name := "shortener_cache_decisions_total"
	s.Equal(before.value(name, map[string]string{"decision": decision})+1, after.value(name, map[string]string{"decision": decision}))
	s.Equal(before.total(name)+1, after.total(name), "one decision per read")
}

func (s *CacheMetricsSuite) assertSource(before, after httpMetricFamilies, source string) {
	s.T().Helper()
	name := "shortener_resolution_success_total"
	s.Equal(before.value(name, map[string]string{"source": source})+1, after.value(name, map[string]string{"source": source}))
	s.Equal(before.total(name)+1, after.total(name))
}

func (s *CacheMetricsSuite) TestMissHitAndInvalidation() {
	item := s.create("cache-metrics")
	before := s.http.httpMetrics(s.T())
	headers, _ := s.http.expect(s.T(), http.MethodGet, "/cache-metrics", "", "", "", http.StatusFound)
	s.Equal("https://example.com/path", headers.Get("Location"))
	after := s.http.httpMetrics(s.T())
	s.assertOperation(before, after, "get", "miss")
	s.assertOperation(before, after, "set", "success")
	s.assertDecision(before, after, "miss")
	s.assertSource(before, after, "database")

	before = after
	headers, _ = s.http.expect(s.T(), http.MethodHead, "/cache-metrics", "", "", "", http.StatusFound)
	s.Equal("https://example.com/path", headers.Get("Location"))
	after = s.http.httpMetrics(s.T())
	s.assertOperation(before, after, "get", "success")
	s.assertDecision(before, after, "hit")
	s.assertSource(before, after, "cache")
	s.Equal(before.total("shortener_cache_operations_total")+1, after.total("shortener_cache_operations_total"))

	before = after
	path := fmt.Sprintf("/api/v1/links/%d", item.ID)
	s.http.expect(s.T(), http.MethodPatch, path, `{"destination":"https://new.example/path"}`, apiKey, "application/json", http.StatusOK)
	after = s.http.httpMetrics(s.T())
	s.assertOperation(before, after, "delete", "success")
	before = after
	headers, _ = s.http.expect(s.T(), http.MethodGet, "/cache-metrics", "", "", "", http.StatusFound)
	s.Equal("https://new.example/path", headers.Get("Location"))
	after = s.http.httpMetrics(s.T())
	s.assertDecision(before, after, "miss")
	s.assertSource(before, after, "database")

	before = after
	s.http.expect(s.T(), http.MethodDelete, path, "", apiKey, "", http.StatusNoContent)
	after = s.http.httpMetrics(s.T())
	s.assertOperation(before, after, "delete", "success")
	before = after
	s.http.expect(s.T(), http.MethodGet, "/cache-metrics", "", "", "", http.StatusNotFound)
	after = s.http.httpMetrics(s.T())
	s.assertDecision(before, after, "miss")
	s.Equal(before.total("shortener_resolution_success_total"), after.total("shortener_resolution_success_total"))

	for name, family := range after {
		if !strings.HasPrefix(name, "shortener_cache_") && !strings.HasPrefix(name, "shortener_resolution_") {
			continue
		}
		for _, metric := range family.GetMetric() {
			for _, label := range metric.GetLabel() {
				for _, secret := range []string{"cache-metrics", "example.com", "new.example", apiKey} {
					s.NotContains(label.GetValue(), secret)
				}
			}
		}
	}
}

func (s *CacheMetricsSuite) TestRejectedSnapshots() {
	s.create("snapshot-metrics")
	future := time.Now().UTC().Add(time.Hour).Format(time.RFC3339Nano)
	past := time.Now().UTC().Add(-time.Hour).Format(time.RFC3339Nano)
	cases := []struct {
		name, snapshot, decision, operationResult string
	}{
		{"malformed", `{`, "invalid", "success"},
		{"empty destination", fmt.Sprintf(`{"destination":"","active":true,"fresh_until":%q}`, future), "invalid", "success"},
		{"stale takes precedence", fmt.Sprintf(`{"destination":"","active":false,"fresh_until":%q,"expires_at":%q}`, past, past), "stale", "success"},
		{"inactive takes precedence", fmt.Sprintf(`{"destination":"","active":false,"fresh_until":%q,"expires_at":%q}`, future, past), "inactive", "success"},
		{"expired", fmt.Sprintf(`{"destination":"https://example.com/expired","active":true,"fresh_until":%q,"expires_at":%q}`, future, past), "expired", "success"},
	}
	for _, tc := range cases {
		s.Run(tc.name, func() {
			s.Equal("OK", s.redis("SET", "shortener:redirect:v1:snapshot-metrics", tc.snapshot))
			before := s.http.httpMetrics(s.T())
			headers, _ := s.http.expect(s.T(), http.MethodGet, "/snapshot-metrics", "", "", "", http.StatusFound)
			s.Equal("https://example.com/path", headers.Get("Location"))
			after := s.http.httpMetrics(s.T())
			s.assertOperation(before, after, "get", tc.operationResult)
			s.assertOperation(before, after, "set", "success")
			s.assertDecision(before, after, tc.decision)
			s.assertSource(before, after, "database")
		})
	}
}

func (s *CacheMetricsSuite) TestRedisOutageReportsFallbackAndMutationFailures() {
	item := s.create("outage-metrics")
	out, err := s.http.run("stop", "redis")
	s.Require().NoError(err, "%s", out)
	defer func() {
		out, restartErr := s.http.run("up", "-d", "--wait", "redis")
		require.NoError(s.T(), restartErr, "%s", out)
	}()

	before := s.http.httpMetrics(s.T())
	headers, _ := s.http.expect(s.T(), http.MethodGet, "/outage-metrics", "", "", "", http.StatusFound)
	s.Equal("https://example.com/path", headers.Get("Location"))
	after := s.http.httpMetrics(s.T())
	s.assertOperation(before, after, "get", "error")
	s.assertOperation(before, after, "set", "error")
	s.assertDecision(before, after, "error")
	s.assertSource(before, after, "database")
	s.Equal(before.value("shortener_cache_failures_total", map[string]string{"operation": "fill"})+1, after.value("shortener_cache_failures_total", map[string]string{"operation": "fill"}))

	path := fmt.Sprintf("/api/v1/links/%d", item.ID)
	before = after
	s.http.expect(s.T(), http.MethodPatch, path, `{"destination":"https://new.example/path"}`, apiKey, "application/json", http.StatusOK)
	after = s.http.httpMetrics(s.T())
	s.assertOperation(before, after, "delete", "error")
	s.Equal(before.value("shortener_cache_failures_total", map[string]string{"operation": "invalidation"})+1, after.value("shortener_cache_failures_total", map[string]string{"operation": "invalidation"}))

	before = after
	s.http.expect(s.T(), http.MethodDelete, path, "", apiKey, "", http.StatusNoContent)
	after = s.http.httpMetrics(s.T())
	s.assertOperation(before, after, "delete", "error")
	s.Equal(before.value("shortener_cache_failures_total", map[string]string{"operation": "invalidation"})+1, after.value("shortener_cache_failures_total", map[string]string{"operation": "invalidation"}))
}
