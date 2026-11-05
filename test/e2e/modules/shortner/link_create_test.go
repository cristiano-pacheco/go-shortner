//go:build e2e

package shortner_test

import (
	"context"
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
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	var out []byte
	err := s.postgresPool.QueryRow(ctx,
		`SELECT COALESCE(json_agg(row_to_json(l) ORDER BY id), '[]'::json) FROM links l`,
	).Scan(&out)
	s.Require().NoError(err, "read persisted links")

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

func (s *CreateLinkSuite) rejectedCreate(body, key, media string, status int, code, message string) {
	s.T().Helper()
	_, responseBody := s.http.expect(s.T(), http.MethodPost, "/api/v1/links", body, key, media, status)
	var response struct {
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	s.Require().NoError(json.Unmarshal(responseBody, &response))
	s.Equal(code, response.Error.Code)
	s.Equal(message, response.Error.Message)
	s.Empty(s.persistedLinks())
}

func (s *CreateLinkSuite) TestRejectsMissingAuthentication() {
	s.rejectedCreate(`{"destination":"https://example.com"}`, "", "application/json", 401, "SHORTENER_29", "API key is missing or invalid")
}

func (s *CreateLinkSuite) TestRejectsInvalidAuthentication() {
	s.rejectedCreate(`{"destination":"https://example.com"}`, "wrong", "application/json", 401, "SHORTENER_29", "API key is missing or invalid")
}

func (s *CreateLinkSuite) TestRejectsUnsupportedMedia() {
	s.rejectedCreate(`{}`, apiKey, "text/plain", 415, "SHORTENER_26", "Content-Type header is not application/json")
}

func (s *CreateLinkSuite) TestRejectsUnknownField() {
	s.rejectedCreate(`{"destination":"https://example.com","unknown":true}`, apiKey, "application/json", 400, "SHORTENER_56", "request body contains an unknown field")
}

func (s *CreateLinkSuite) TestRejectsMalformedJSON() {
	s.rejectedCreate(`{"destination":`, apiKey, "application/json", 400, "SHORTENER_28", "request body contains malformed JSON")
}

func (s *CreateLinkSuite) TestRejectsTrailingJSON() {
	s.rejectedCreate(`{} {}`, apiKey, "application/json", 400, "SHORTENER_58", "request body must contain only one JSON value")
}

func (s *CreateLinkSuite) TestRejectsOversizedBody() {
	s.rejectedCreate(
		`{"destination":"https://example.com/`+strings.Repeat("a", 1<<20)+`"}`,
		apiKey,
		"application/json",
		413,
		"SHORTENER_27",
		"Request body is too large",
	)
}

func (s *CreateLinkSuite) TestRejectsMissingDestination() {
	s.rejectedCreate(`{}`, apiKey, "application/json", 400, "SHORTENER_31", "destination is required")
}

func (s *CreateLinkSuite) TestRejectsEmptyBody() {
	s.rejectedCreate(``, apiKey, "application/json", 400, "SHORTENER_55", "request body must not be empty")
}

func (s *CreateLinkSuite) TestRejectsInvalidFieldType() {
	s.rejectedCreate(`{"destination":42}`, apiKey, "application/json", 400, "SHORTENER_60", "destination must be a string")
}

func (s *CreateLinkSuite) TestRejectsInvalidTimestamp() {
	s.rejectedCreate(`{"destination":"https://example.com","expires_at":"tomorrow"}`, apiKey, "application/json", 400, "SHORTENER_48", "expiration must be an RFC3339 timestamp or null")
}

func (s *CreateLinkSuite) TestRejectsRelativeDestination() {
	s.rejectedCreate(`{"destination":"/relative"}`, apiKey, "application/json", 400, "SHORTENER_33", "destination scheme must be HTTP or HTTPS")
}

func (s *CreateLinkSuite) TestRejectsUnsupportedScheme() {
	s.rejectedCreate(`{"destination":"ftp://example.com"}`, apiKey, "application/json", 400, "SHORTENER_33", "destination scheme must be HTTP or HTTPS")
}

func (s *CreateLinkSuite) TestRejectsDestinationCredentials() {
	s.rejectedCreate(`{"destination":"https://u:p@example.com"}`, apiKey, "application/json", 400, "SHORTENER_35", "destination must not include credentials")
}

func (s *CreateLinkSuite) TestRejectsDestinationWhitespace() {
	s.rejectedCreate(`{"destination":"https://example.com/ "}`, apiKey, "application/json", 400, "SHORTENER_36", "destination must not contain whitespace")
}

func (s *CreateLinkSuite) TestRejectsShortAlias() {
	s.rejectedCreate(`{"destination":"https://example.com","alias":"ab"}`, apiKey, "application/json", 400, "SHORTENER_38", "alias must be between 3 and 32 characters")
}

func (s *CreateLinkSuite) TestRejectsLongAlias() {
	s.rejectedCreate(
		`{"destination":"https://example.com","alias":"`+strings.Repeat("a", 33)+`"}`,
		apiKey,
		"application/json",
		400,
		"SHORTENER_38",
		"alias must be between 3 and 32 characters",
	)
}

func (s *CreateLinkSuite) TestRejectsInvalidAlias() {
	s.rejectedCreate(`{"destination":"https://example.com","alias":"abc.def"}`, apiKey, "application/json", 400, "SHORTENER_40", "alias may only contain letters, numbers, hyphens, and underscores")
}

func (s *CreateLinkSuite) TestRejectsReservedAlias() {
	s.rejectedCreate(`{"destination":"https://example.com","alias":"health"}`, apiKey, "application/json", 400, "SHORTENER_39", "alias is reserved")
}

func (s *CreateLinkSuite) TestRejectsExpiredLink() {
	s.rejectedCreate(
		`{"destination":"https://example.com","expires_at":"2020-01-01T00:00:00Z"}`,
		apiKey,
		"application/json",
		400,
		"SHORTENER_41",
		"expiration must be in the future",
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
	_, body := s.http.expect(
		s.T(),
		http.MethodPost,
		"/api/v1/links",
		`{"destination":"https://other.example","alias":"duplicate-e2e"}`,
		apiKey,
		"application/json",
		http.StatusConflict,
	)
	var response struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	s.Require().NoError(json.Unmarshal(body, &response))
	s.Equal("SHORTENER_02", response.Error.Code)
	s.Equal(before, s.persistedLinks())
}
