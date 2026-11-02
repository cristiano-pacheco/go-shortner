//go:build e2e

package shortner_test

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/suite"
)

type DeleteLinkSuite struct {
	endpointSuite
}

func TestDeleteLinkSuite(t *testing.T) {
	suite.Run(t, new(DeleteLinkSuite))
}

func (s *DeleteLinkSuite) fixture() []persistedLink {
	s.T().Helper()
	s.sql(`INSERT INTO links (id, code, destination, active, expires_at, created_at, updated_at) VALUES
 (1, 'delete-e2e', 'https://example.com/path', true, '2099-01-01 00:00:00+00', '2021-01-01 00:00:00+00', '2021-01-01 00:00:00+00'),
 (2, 'other-e2e', 'https://example.com/other', true, NULL, '2021-01-01 00:00:00+00', '2021-01-01 00:00:00+00')`)

	return s.persistedLinks()
}

func (s *DeleteLinkSuite) TestSoftDeletePreservesRowAndOtherLinks() {
	before := s.fixture()
	s.Require().Len(before, 2)
	s.http.expect(s.T(), http.MethodGet, "/delete-e2e", "", "", "", http.StatusFound)
	_, body := s.http.expect(s.T(), http.MethodDelete, "/api/v1/links/1", "", apiKey, "", http.StatusNoContent)
	s.Empty(body)
	after := s.persistedLinks()
	s.Require().Len(after, 2)
	s.Require().NotNil(after[0].DeletedAt)
	s.True(after[0].UpdatedAt.After(before[0].UpdatedAt))
	s.True(after[0].DeletedAt.Equal(after[0].UpdatedAt))
	s.Equal(before[0].ID, after[0].ID)
	s.Equal(before[0].Code, after[0].Code)
	s.Equal(before[0].Destination, after[0].Destination)
	s.Equal(before[0].Active, after[0].Active)
	s.Equal(before[0].ExpiresAt, after[0].ExpiresAt)
	s.Equal(before[0].CreatedAt, after[0].CreatedAt)
	s.Equal(before[1], after[1])
	s.http.expect(s.T(), http.MethodGet, "/delete-e2e", "", "", "", http.StatusNotFound)
}

func (s *DeleteLinkSuite) TestRepeatedDeleteDoesNotChangeDeletedRow() {
	s.fixture()
	s.http.expect(s.T(), http.MethodDelete, "/api/v1/links/1", "", apiKey, "", http.StatusNoContent)
	before := s.persistedLinks()
	s.http.expect(s.T(), http.MethodDelete, "/api/v1/links/1", "", apiKey, "", http.StatusNotFound)
	s.Equal(before, s.persistedLinks())
}

func (s *DeleteLinkSuite) TestDeletedAliasRemainsReserved() {
	s.fixture()
	s.http.expect(s.T(), http.MethodDelete, "/api/v1/links/1", "", apiKey, "", http.StatusNoContent)
	before := s.persistedLinks()
	s.http.expect(
		s.T(),
		http.MethodPost,
		"/api/v1/links",
		`{"destination":"https://example.com","alias":"delete-e2e"}`,
		apiKey,
		"application/json",
		http.StatusConflict,
	)
	s.Equal(before, s.persistedLinks())
}

func (s *DeleteLinkSuite) rejectedDelete(path, key string, status int) {
	s.T().Helper()
	before := s.fixture()
	s.http.expect(s.T(), http.MethodDelete, path, "", key, "", status)
	s.Equal(before, s.persistedLinks())
}

func (s *DeleteLinkSuite) TestMissingAuthenticationLeavesDatabaseUnchanged() {
	s.rejectedDelete("/api/v1/links/1", "", http.StatusUnauthorized)
}

func (s *DeleteLinkSuite) TestInvalidAuthenticationLeavesDatabaseUnchanged() {
	s.rejectedDelete("/api/v1/links/1", "wrong", http.StatusUnauthorized)
}

func (s *DeleteLinkSuite) TestNonNumericIDLeavesDatabaseUnchanged() {
	s.rejectedDelete("/api/v1/links/abc", apiKey, http.StatusBadRequest)
}

func (s *DeleteLinkSuite) TestZeroIDLeavesDatabaseUnchanged() {
	s.rejectedDelete("/api/v1/links/0", apiKey, http.StatusBadRequest)
}

func (s *DeleteLinkSuite) TestNegativeIDLeavesDatabaseUnchanged() {
	s.rejectedDelete("/api/v1/links/-1", apiKey, http.StatusBadRequest)
}

func (s *DeleteLinkSuite) TestOverflowIDLeavesDatabaseUnchanged() {
	s.rejectedDelete("/api/v1/links/9223372036854775808", apiKey, http.StatusBadRequest)
}

func (s *DeleteLinkSuite) TestMissingLinkLeavesDatabaseUnchanged() {
	s.rejectedDelete("/api/v1/links/999999999", apiKey, http.StatusNotFound)
}
