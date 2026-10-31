//go:build e2e

package shortner_test

import (
	"github.com/stretchr/testify/suite"
	"testing"
)

import "fmt"

type DeleteLinkSuite struct {
	endpointSuite
}

func TestDeleteLinkSuite(t *testing.T) {
	suite.Run(t, new(DeleteLinkSuite))
}

func (s *DeleteLinkSuite) TestDeleteLink() {
	t := s.T()
	item := s.create("delete-e2e")
	path := fmt.Sprintf("/api/v1/links/%d", item.ID)
	for _, key := range []string{"", "wrong"} {
		s.http.expect(t, "DELETE", path, "", key, "", 401)
	}
	for _, id := range []string{"abc", "0", "-1", "9223372036854775808"} {
		s.http.expect(t, "DELETE", "/api/v1/links/"+id, "", apiKey, "", 400)
	}
	s.http.expect(t, "DELETE", "/api/v1/links/999999999", "", apiKey, "", 404)
	s.http.expect(t, "GET", "/delete-e2e", "", "", "", 302)
	_, b := s.http.expect(t, "DELETE", path, "", apiKey, "", 204)
	s.Empty(b)
	s.http.expect(t, "GET", "/delete-e2e", "", "", "", 404)
	s.http.expect(t, "DELETE", path, "", apiKey, "", 404)
	s.http.expect(t, "PATCH", path, `{"active":true}`, apiKey, "application/json", 404)
	s.http.expect(t, "POST", "/api/v1/links", `{"destination":"https://example.com","alias":"delete-e2e"}`, apiKey, "application/json", 409)
}
