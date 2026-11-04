//go:build e2e

package shortner_test

import (
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/suite"
)

type GetLinkSuite struct {
	endpointSuite
}

//nolint:paralleltest // Management suites share a Compose stack.
func TestGetLinkSuite(t *testing.T) {
	suite.Run(t, new(GetLinkSuite))
}

func (s *GetLinkSuite) seedGetLinks() {
	s.T().Helper()
	s.sql(`INSERT INTO links (id, code, destination, active, expires_at, created_at, updated_at, deleted_at) VALUES
 (1, 'first-link', 'https://example.com/first', true, NULL, '2021-01-01 00:00:00+00', '2021-02-01 00:00:00+00', NULL),
 (2, 'inactive-link', 'https://example.com/inactive', false, '2099-01-01 00:00:00+00', '2021-01-02 00:00:00+00', '2021-02-02 00:00:00+00', NULL),
 (3, 'expired-link', 'https://example.com/expired', true, '2020-01-01 00:00:00+00', '2021-01-03 00:00:00+00', '2021-02-03 00:00:00+00', NULL),
 (4, 'deleted-link', 'https://example.com/deleted', true, NULL, '2021-01-04 00:00:00+00', '2021-02-04 00:00:00+00', '2021-03-01 00:00:00+00')`)
}

func (s *GetLinkSuite) TestGetExistingLinkReturnsAllFields() {
	s.seedGetLinks()
	_, body := s.http.expect(s.T(), http.MethodGet, "/api/v1/links/1", "", apiKey, "", http.StatusOK)
	item := decodeLink(s.T(), body)
	s.Equal(int64(1), item.ID)
	s.Equal("first-link", item.Code)
	s.Equal("https://example.com/first", item.Destination)
	s.Equal(publicBase+"/first-link", item.ShortURL)
	s.True(item.Active)
	s.Nil(item.ExpiresAt)
	s.Equal(time.Date(2021, 1, 1, 0, 0, 0, 0, time.UTC), item.CreatedAt)
	s.Equal(time.Date(2021, 2, 1, 0, 0, 0, 0, time.UTC), item.UpdatedAt)
}

func (s *GetLinkSuite) TestGetInactiveLink() {
	s.seedGetLinks()
	_, body := s.http.expect(s.T(), http.MethodGet, "/api/v1/links/2", "", apiKey, "", http.StatusOK)
	item := decodeLink(s.T(), body)
	s.Equal(int64(2), item.ID)
	s.Equal("inactive-link", item.Code)
	s.Equal("https://example.com/inactive", item.Destination)
	s.False(item.Active)

	if s.NotNil(item.ExpiresAt) {
		s.True(item.ExpiresAt.Equal(time.Date(2099, 1, 1, 0, 0, 0, 0, time.UTC)))
	}
}

func (s *GetLinkSuite) TestGetExpiredLink() {
	s.seedGetLinks()
	_, body := s.http.expect(s.T(), http.MethodGet, "/api/v1/links/3", "", apiKey, "", http.StatusOK)
	item := decodeLink(s.T(), body)
	s.Equal(int64(3), item.ID)
	s.Equal("expired-link", item.Code)
	s.True(item.Active)

	if s.NotNil(item.ExpiresAt) {
		s.True(item.ExpiresAt.Equal(time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)))
	}
}

func (s *GetLinkSuite) TestGetSoftDeletedLinkReturnsNotFound() {
	s.seedGetLinks()
	s.http.expect(s.T(), http.MethodGet, "/api/v1/links/4", "", apiKey, "", http.StatusNotFound)
}

func (s *GetLinkSuite) TestGetMissingLinkReturnsNotFound() {
	s.seedGetLinks()
	s.http.expect(s.T(), http.MethodGet, "/api/v1/links/999", "", apiKey, "", http.StatusNotFound)
}

func (s *GetLinkSuite) TestGetWithoutAPIKeyIsUnauthorized() {
	s.seedGetLinks()
	s.http.expect(s.T(), http.MethodGet, "/api/v1/links/1", "", "", "", http.StatusUnauthorized)
}

func (s *GetLinkSuite) TestGetWithWrongAPIKeyIsUnauthorized() {
	s.seedGetLinks()
	s.http.expect(s.T(), http.MethodGet, "/api/v1/links/1", "", "wrong", "", http.StatusUnauthorized)
}

func (s *GetLinkSuite) TestGetWithNonNumericIDReturnsBadRequest() {
	s.http.expect(s.T(), http.MethodGet, "/api/v1/links/not-a-number", "", apiKey, "", http.StatusBadRequest)
}

func (s *GetLinkSuite) TestGetWithZeroIDReturnsBadRequest() {
	s.http.expect(s.T(), http.MethodGet, "/api/v1/links/0", "", apiKey, "", http.StatusBadRequest)
}

func (s *GetLinkSuite) TestGetWithNegativeIDReturnsBadRequest() {
	s.http.expect(s.T(), http.MethodGet, "/api/v1/links/-1", "", apiKey, "", http.StatusBadRequest)
}

func (s *GetLinkSuite) TestGetWithOverflowIDReturnsBadRequest() {
	s.http.expect(s.T(), http.MethodGet, "/api/v1/links/9223372036854775808", "", apiKey, "", http.StatusBadRequest)
}
