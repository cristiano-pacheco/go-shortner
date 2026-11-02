//go:build e2e

package shortner_test

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/suite"
)

type UpdateLinkSuite struct {
	endpointSuite
}

func TestUpdateLinkSuite(t *testing.T) {
	suite.Run(t, new(UpdateLinkSuite))
}

func (s *UpdateLinkSuite) fixture() []persistedLink {
	s.T().Helper()
	s.sql(`INSERT INTO links (id, code, destination, active, expires_at, created_at, updated_at) VALUES
 (1, 'update-e2e', 'https://example.com/original', true, '2099-01-01 00:00:00+00', '2021-01-01 00:00:00+00', '2021-01-01 00:00:00+00'),
 (2, 'other-e2e', 'https://example.com/other', true, NULL, '2021-01-01 00:00:00+00', '2021-01-01 00:00:00+00')`)

	return s.persistedLinks()
}

func (s *UpdateLinkSuite) patch(body string) link {
	s.T().Helper()
	_, raw := s.http.expect(s.T(), http.MethodPatch, "/api/v1/links/1", body, apiKey, "application/json", http.StatusOK)

	return decodeLink(s.T(), raw)
}

func (s *UpdateLinkSuite) TestUpdateDestinationPersistsAndInvalidatesCache() {
	before := s.fixture()
	s.http.expect(s.T(), http.MethodGet, "/update-e2e", "", "", "", http.StatusFound)
	updated := s.patch(`{"destination":"https://new.example/path"}`)
	after := s.persistedLinks()
	s.Require().Len(after, 2)
	s.Equal("https://new.example/path", updated.Destination)
	s.Equal("https://new.example/path", after[0].Destination)
	s.Equal(before[0].Code, after[0].Code)
	s.Equal(before[0].Active, after[0].Active)
	s.Equal(before[0].ExpiresAt, after[0].ExpiresAt)
	s.Equal(before[0].CreatedAt, after[0].CreatedAt)
	s.True(after[0].UpdatedAt.After(before[0].UpdatedAt))
	s.Nil(after[0].DeletedAt)
	s.Equal(before[1], after[1])
	header, _ := s.http.expect(s.T(), http.MethodGet, "/update-e2e", "", "", "", http.StatusFound)
	s.Equal("https://new.example/path", header.Get("Location"))
}

func (s *UpdateLinkSuite) TestDeactivatePersistsWithoutChangingOtherFields() {
	before := s.fixture()
	updated := s.patch(`{"active":false}`)
	after := s.persistedLinks()
	s.Require().Len(after, 2)
	s.False(updated.Active)
	s.False(after[0].Active)
	s.Equal(before[0].Destination, after[0].Destination)
	s.Equal(before[0].ExpiresAt, after[0].ExpiresAt)
	s.Equal(before[0].Code, after[0].Code)
	s.True(after[0].UpdatedAt.After(before[0].UpdatedAt))
	s.Equal(before[1], after[1])
	s.http.expect(s.T(), http.MethodGet, "/update-e2e", "", "", "", http.StatusNotFound)
}

func (s *UpdateLinkSuite) TestMultipleFieldsUpdatedTogether() {
	before := s.fixture()
	s.http.expect(s.T(), http.MethodGet, "/update-e2e", "", "", "", http.StatusFound)
	updated := s.patch(`{"destination":"https://new.example/combined","active":false,"expires_at":null}`)
	after := s.persistedLinks()
	s.Require().Len(after, 2)
	s.Equal("https://new.example/combined", updated.Destination)
	s.False(updated.Active)
	s.Nil(updated.ExpiresAt)
	s.Equal("https://new.example/combined", after[0].Destination)
	s.False(after[0].Active)
	s.Nil(after[0].ExpiresAt)
	s.Equal(before[0].ID, after[0].ID)
	s.Equal(before[0].Code, after[0].Code)
	s.Equal(before[0].CreatedAt, after[0].CreatedAt)
	s.True(after[0].UpdatedAt.After(before[0].UpdatedAt))
	s.Nil(after[0].DeletedAt)
	s.Equal(before[1], after[1])
	s.http.expect(s.T(), http.MethodGet, "/update-e2e", "", "", "", http.StatusNotFound)
}

func (s *UpdateLinkSuite) TestReactivateLinkRestoresRedirect() {
	s.fixture()
	s.sql(`UPDATE links SET active = false WHERE id = 1`)
	before := s.persistedLinks()
	s.http.expect(s.T(), http.MethodGet, "/update-e2e", "", "", "", http.StatusNotFound)
	updated := s.patch(`{"active":true}`)
	after := s.persistedLinks()
	s.Require().Len(after, 2)
	s.True(updated.Active)
	s.True(after[0].Active)
	s.Equal(before[0].ID, after[0].ID)
	s.Equal(before[0].Code, after[0].Code)
	s.Equal(before[0].Destination, after[0].Destination)
	s.Equal(before[0].ExpiresAt, after[0].ExpiresAt)
	s.Equal(before[0].CreatedAt, after[0].CreatedAt)
	s.True(after[0].UpdatedAt.After(before[0].UpdatedAt))
	s.Nil(after[0].DeletedAt)
	s.Equal(before[1], after[1])
	header, _ := s.http.expect(s.T(), http.MethodGet, "/update-e2e", "", "", "", http.StatusFound)
	s.Equal(before[0].Destination, header.Get("Location"))
}

func (s *UpdateLinkSuite) TestExpirationCanBeChanged() {
	before := s.fixture()
	updated := s.patch(`{"expires_at":"2098-01-01T00:00:00Z"}`)
	after := s.persistedLinks()
	s.Require().Len(after, 2)

	expected := time.Date(2098, 1, 1, 0, 0, 0, 0, time.UTC)

	s.Require().NotNil(updated.ExpiresAt)
	s.True(updated.ExpiresAt.Equal(expected))
	s.Require().NotNil(after[0].ExpiresAt)
	s.True(after[0].ExpiresAt.Equal(expected))
	s.Equal(before[0].Destination, after[0].Destination)
	s.Equal(before[0].Active, after[0].Active)
	s.Equal(before[1], after[1])
}

func (s *UpdateLinkSuite) TestExpirationCanBeCleared() {
	before := s.fixture()
	updated := s.patch(`{"expires_at":null}`)
	after := s.persistedLinks()
	s.Require().Len(after, 2)
	s.Nil(updated.ExpiresAt)
	s.Nil(after[0].ExpiresAt)
	s.Equal(before[0].Destination, after[0].Destination)
	s.Equal(before[0].Active, after[0].Active)
	s.Equal(before[1], after[1])
}

func (s *UpdateLinkSuite) rejectedPatch(path, body, key string, status int) {
	s.T().Helper()
	before := s.fixture()
	s.http.expect(s.T(), http.MethodPatch, path, body, key, "application/json", status)
	s.Equal(before, s.persistedLinks())
}

func (s *UpdateLinkSuite) TestMissingAuthenticationLeavesDatabaseUnchanged() {
	s.rejectedPatch("/api/v1/links/1", `{"active":false}`, "", 401)
}

func (s *UpdateLinkSuite) TestInvalidAuthenticationLeavesDatabaseUnchanged() {
	s.rejectedPatch("/api/v1/links/1", `{"active":false}`, "wrong", 401)
}

func (s *UpdateLinkSuite) TestUnknownFieldLeavesDatabaseUnchanged() {
	s.rejectedPatch("/api/v1/links/1", `{"code":"other"}`, apiKey, 400)
}

func (s *UpdateLinkSuite) TestInvalidDestinationLeavesDatabaseUnchanged() {
	before := s.fixture()
	_, body := s.http.expect(s.T(), http.MethodPatch, "/api/v1/links/1", `{"destination":"ftp://example.com","active":false}`, apiKey, "application/json", http.StatusBadRequest)
	var response struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	s.Require().NoError(json.Unmarshal(body, &response))
	s.Equal("invalid_argument", response.Error.Code)
	s.Equal(before, s.persistedLinks())
}

func (s *UpdateLinkSuite) TestNullActiveLeavesDatabaseUnchanged() {
	s.rejectedPatch("/api/v1/links/1", `{"active":null}`, apiKey, 400)
}

func (s *UpdateLinkSuite) TestNullDestinationLeavesDatabaseUnchanged() {
	s.rejectedPatch("/api/v1/links/1", `{"destination":null}`, apiKey, 400)
}

func (s *UpdateLinkSuite) TestPastExpirationLeavesDatabaseUnchanged() {
	s.rejectedPatch("/api/v1/links/1", `{"expires_at":"2020-01-01T00:00:00Z"}`, apiKey, 400)
}

func (s *UpdateLinkSuite) TestMalformedBodyLeavesDatabaseUnchanged() {
	s.rejectedPatch("/api/v1/links/1", `{"active":false`, apiKey, 400)
}

func (s *UpdateLinkSuite) TestNonNumericIDLeavesDatabaseUnchanged() {
	s.rejectedPatch("/api/v1/links/abc", `{"active":false}`, apiKey, 400)
}

func (s *UpdateLinkSuite) TestZeroIDLeavesDatabaseUnchanged() {
	s.rejectedPatch("/api/v1/links/0", `{"active":false}`, apiKey, 400)
}

func (s *UpdateLinkSuite) TestNegativeIDLeavesDatabaseUnchanged() {
	s.rejectedPatch("/api/v1/links/-1", `{"active":false}`, apiKey, 400)
}

func (s *UpdateLinkSuite) TestOverflowIDLeavesDatabaseUnchanged() {
	s.rejectedPatch("/api/v1/links/9223372036854775808", `{"active":false}`, apiKey, 400)
}

func (s *UpdateLinkSuite) TestMissingLinkLeavesDatabaseUnchanged() {
	s.rejectedPatch("/api/v1/links/999999999", `{"active":false}`, apiKey, 404)
}

func (s *UpdateLinkSuite) TestDeletedLinkCannotBeUpdated() {
	s.fixture()
	s.sql(`UPDATE links SET deleted_at = '2022-01-01 00:00:00+00' WHERE id = 1`)
	before := s.persistedLinks()
	s.http.expect(
		s.T(),
		http.MethodPatch,
		"/api/v1/links/1",
		`{"active":false}`,
		apiKey,
		"application/json",
		http.StatusNotFound,
	)
	s.Equal(before, s.persistedLinks())
}
