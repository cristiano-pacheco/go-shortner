//go:build e2e

package shortner_test

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/suite"
)

type persistedLink struct {
	ID          int64      `json:"id"`
	Code        string     `json:"code"`
	Destination string     `json:"destination"`
	Active      bool       `json:"active"`
	ExpiresAt   *time.Time `json:"expires_at"`
	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
	DeletedAt   *time.Time `json:"deleted_at"`
}

func (s *endpointSuite) persistedLinks() []persistedLink {
	s.T().Helper()
	out, err := s.http.run(
		"exec",
		"-T",
		"postgres",
		"psql",
		"-X",
		"-A",
		"-t",
		"-v",
		"ON_ERROR_STOP=1",
		"-U",
		"shortener",
		"-d",
		"shortener",
		"-c",
		`SELECT COALESCE(json_agg(row_to_json(l) ORDER BY id), '[]'::json) FROM links l`,
	)
	s.Require().NoError(err, "read persisted links: %s", out)

	var links []persistedLink
	s.Require().NoError(json.Unmarshal(out, &links), "database response: %s", out)

	return links
}

type CreateLinkSuite struct {
	endpointSuite
}

//nolint:paralleltest // Management suites share a Compose stack.
func TestCreateLinkSuite(t *testing.T) {
	suite.Run(t, new(CreateLinkSuite))
}

func (s *CreateLinkSuite) TestGeneratedCodeIsPersisted() {
	_, body := s.http.expect(
		s.T(),
		http.MethodPost,
		"/api/v1/links",
		`{"destination":"https://example.com/path"}`,
		apiKey,
		"application/json",
		http.StatusCreated,
	)
	item := decodeLink(s.T(), body)
	s.Regexp(`^[A-Za-z0-9]{8}$`, item.Code)
	s.Positive(item.ID)
	s.Equal("https://example.com/path", item.Destination)
	s.Equal(publicBase+"/"+item.Code, item.ShortURL)
	s.True(item.Active)
	s.Nil(item.ExpiresAt)
	s.False(item.CreatedAt.IsZero())
	s.False(item.UpdatedAt.IsZero())

	rows := s.persistedLinks()
	s.Require().Len(rows, 1)
	s.Equal(item.ID, rows[0].ID)
	s.Equal(item.Code, rows[0].Code)
	s.Equal("https://example.com/path", rows[0].Destination)
	s.True(rows[0].Active)
	s.Nil(rows[0].ExpiresAt)
	s.Nil(rows[0].DeletedAt)
	s.True(item.CreatedAt.Equal(rows[0].CreatedAt))
	s.True(item.UpdatedAt.Equal(rows[0].UpdatedAt))
}

func (s *CreateLinkSuite) TestAliasAndExpirationArePersisted() {
	_, body := s.http.expect(
		s.T(),
		http.MethodPost,
		"/api/v1/links",
		`{"destination":"https://example.com/a?x=1&y=two","alias":"Case_123","expires_at":"2099-01-01T00:00:00Z"}`,
		apiKey,
		"application/json",
		http.StatusCreated,
	)
	item := decodeLink(s.T(), body)
	s.Equal("Case_123", item.Code)
	s.Equal("https://example.com/a?x=1&y=two", item.Destination)
	s.Equal(publicBase+"/Case_123", item.ShortURL)

	expires := time.Date(2099, 1, 1, 0, 0, 0, 0, time.UTC)

	s.Require().NotNil(item.ExpiresAt)
	s.True(expires.Equal(*item.ExpiresAt))
	rows := s.persistedLinks()
	s.Require().Len(rows, 1)
	s.Equal(item.ID, rows[0].ID)
	s.Equal("Case_123", rows[0].Code)
	s.Equal(item.Destination, rows[0].Destination)
	s.True(rows[0].Active)
	s.Require().NotNil(rows[0].ExpiresAt)
	s.True(expires.Equal(*rows[0].ExpiresAt))
	s.Nil(rows[0].DeletedAt)
	s.True(item.CreatedAt.Equal(rows[0].CreatedAt))
	s.True(item.UpdatedAt.Equal(rows[0].UpdatedAt))
}

func (s *CreateLinkSuite) rejectedCreate(body, key, media string, status int) {
	s.T().Helper()
	s.http.expect(s.T(), http.MethodPost, "/api/v1/links", body, key, media, status)
	s.Empty(s.persistedLinks())
}

func (s *CreateLinkSuite) TestRejectsMissingAuthentication() {
	s.rejectedCreate(`{"destination":"https://example.com"}`, "", "application/json", 401)
}

func (s *CreateLinkSuite) TestRejectsInvalidAuthentication() {
	s.rejectedCreate(`{"destination":"https://example.com"}`, "wrong", "application/json", 401)
}

func (s *CreateLinkSuite) TestRejectsUnsupportedMedia() {
	s.rejectedCreate(`{}`, apiKey, "text/plain", 415)
}

func (s *CreateLinkSuite) TestRejectsUnknownField() {
	s.rejectedCreate(`{"destination":"https://example.com","unknown":true}`, apiKey, "application/json", 400)
}

func (s *CreateLinkSuite) TestRejectsMalformedJSON() {
	s.rejectedCreate(`{"destination":`, apiKey, "application/json", 400)
}

func (s *CreateLinkSuite) TestRejectsTrailingJSON() {
	s.rejectedCreate(`{} {}`, apiKey, "application/json", 400)
}

func (s *CreateLinkSuite) TestRejectsOversizedBody() {
	s.rejectedCreate(
		`{"destination":"https://example.com/`+strings.Repeat("a", 1<<20)+`"}`,
		apiKey,
		"application/json",
		413,
	)
}

func (s *CreateLinkSuite) TestRejectsMissingDestination() {
	_, body := s.http.expect(
		s.T(),
		http.MethodPost,
		"/api/v1/links",
		`{}`,
		apiKey,
		"application/json",
		http.StatusBadRequest,
	)

	var response struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	s.Require().NoError(json.Unmarshal(body, &response))
	s.Equal("invalid_argument", response.Error.Code)
	s.Empty(s.persistedLinks())
}

func (s *CreateLinkSuite) TestRejectsRelativeDestination() {
	s.rejectedCreate(`{"destination":"/relative"}`, apiKey, "application/json", 400)
}

func (s *CreateLinkSuite) TestRejectsUnsupportedScheme() {
	s.rejectedCreate(`{"destination":"ftp://example.com"}`, apiKey, "application/json", 400)
}

func (s *CreateLinkSuite) TestRejectsDestinationCredentials() {
	s.rejectedCreate(`{"destination":"https://u:p@example.com"}`, apiKey, "application/json", 400)
}

func (s *CreateLinkSuite) TestRejectsDestinationWhitespace() {
	s.rejectedCreate(`{"destination":"https://example.com/ "}`, apiKey, "application/json", 400)
}

func (s *CreateLinkSuite) TestRejectsShortAlias() {
	s.rejectedCreate(`{"destination":"https://example.com","alias":"ab"}`, apiKey, "application/json", 400)
}

func (s *CreateLinkSuite) TestRejectsLongAlias() {
	s.rejectedCreate(
		`{"destination":"https://example.com","alias":"`+strings.Repeat("a", 33)+`"}`,
		apiKey,
		"application/json",
		400,
	)
}

func (s *CreateLinkSuite) TestRejectsInvalidAlias() {
	s.rejectedCreate(`{"destination":"https://example.com","alias":"abc.def"}`, apiKey, "application/json", 400)
}

func (s *CreateLinkSuite) TestRejectsReservedAlias() {
	s.rejectedCreate(`{"destination":"https://example.com","alias":"health"}`, apiKey, "application/json", 400)
}

func (s *CreateLinkSuite) TestRejectsExpiredLink() {
	s.rejectedCreate(
		`{"destination":"https://example.com","expires_at":"2020-01-01T00:00:00Z"}`,
		apiKey,
		"application/json",
		400,
	)
}

func (s *CreateLinkSuite) TestMigrationRerunPreservesExistingRows() {
	s.create("migration-rerun-e2e")
	before := s.persistedLinks()
	s.Require().Len(before, 1)

	out, err := s.http.run("run", "--rm", "--no-deps", "app", "migrate")
	s.Require().NoError(err, "rerun migrations: %s", out)
	s.Equal(before, s.persistedLinks())
}

func (s *CreateLinkSuite) TestDuplicateAliasDoesNotChangeExistingRow() {
	s.create("duplicate-e2e")
	before := s.persistedLinks()
	s.Require().Len(before, 1)
	s.http.expect(
		s.T(),
		http.MethodPost,
		"/api/v1/links",
		`{"destination":"https://other.example","alias":"duplicate-e2e"}`,
		apiKey,
		"application/json",
		http.StatusConflict,
	)
	s.Equal(before, s.persistedLinks())
}
