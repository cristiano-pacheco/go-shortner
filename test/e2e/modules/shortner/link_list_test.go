//go:build e2e

package shortner_test

import (
	"github.com/stretchr/testify/suite"
	"testing"
)

import (
	"encoding/json"
	"fmt"
	"time"
)

type listResponse struct {
	Data struct {
		Items    []link `json:"items"`
		Page     int    `json:"page"`
		PageSize int    `json:"page_size"`
		Total    int    `json:"total"`
	} `json:"data"`
}

type ListLinkSuite struct {
	endpointSuite
}

func TestListLinkSuite(t *testing.T) {
	suite.Run(t, new(ListLinkSuite))
}

func (s *ListLinkSuite) TestListLinks() {
	t := s.T()
	const path = "/api/v1/links"
	for _, key := range []string{"", "wrong"} {
		s.http.expect(t, "GET", path, "", key, "", 401)
	}
	read := func(query string) listResponse {
		t.Helper()
		_, body := s.http.expect(t, "GET", path+query, "", apiKey, "", 200)
		var response listResponse
		s.Require().NoError(json.Unmarshal(body, &response))
		return response
	}
	empty := read("")
	s.NotNil(empty.Data.Items)
	s.Empty(empty.Data.Items)
	s.Equal(0, empty.Data.Total)
	s.Equal(1, empty.Data.Page)
	s.Equal(20, empty.Data.PageSize)

	// Adjacent rows share a timestamp, forcing the ID tie-breaker.
	s.sql(`INSERT INTO links (code, destination, active, expires_at, created_at, updated_at, deleted_at)
 SELECT 'list-' || n, 'https://destination.example/' || n, n <> 24,
 CASE WHEN n = 23 THEN '2020-01-01 00:00:00+00'::timestamptz ELSE NULL END,
 '2021-01-01 00:00:00+00'::timestamptz + ((n - 1) / 2) * interval '1 second',
 '2021-02-01 00:00:00+00'::timestamptz,
 CASE WHEN n = 25 THEN '2021-03-01 00:00:00+00'::timestamptz ELSE NULL END
 FROM generate_series(1, 25) AS n ORDER BY n`)
	first, second := read(""), read("?page=2")
	s.Equal(24, first.Data.Total)
	s.Equal(24, second.Data.Total)
	s.Equal(1, first.Data.Page)
	s.Equal(2, second.Data.Page)
	s.Equal(20, first.Data.PageSize)
	s.Equal(20, second.Data.PageSize)
	s.Require().Len(first.Data.Items, 20)
	s.Require().Len(second.Data.Items, 4)
	items := append(first.Data.Items, second.Data.Items...)
	for index, item := range items {
		n := 24 - index
		s.Equal(int64(n), item.ID)
		s.Equal(fmt.Sprintf("list-%d", n), item.Code)
		s.Equal(fmt.Sprintf("https://destination.example/%d", n), item.Destination)
		s.Equal(fmt.Sprintf("%s/list-%d", publicBase, n), item.ShortURL)
		s.Equal(n != 24, item.Active)
		if n == 23 {
			if s.NotNil(item.ExpiresAt) {
				s.True(item.ExpiresAt.Equal(time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)))
			}
		} else {
			s.Nil(item.ExpiresAt)
		}
		s.False(item.CreatedAt.IsZero())
		s.False(item.UpdatedAt.IsZero())
	}
	single := read("?page=1&page_size=1")
	s.Len(single.Data.Items, 1)
	s.Equal(24, single.Data.Total)
	s.Equal(1, single.Data.PageSize)
	full := read("?page=1&page_size=100")
	s.Len(full.Data.Items, 24)
	s.Equal(24, full.Data.Total)
	s.Equal(100, full.Data.PageSize)
	beyond := read("?page=100&page_size=1")
	s.NotNil(beyond.Data.Items)
	s.Empty(beyond.Data.Items)
	s.Equal(24, beyond.Data.Total)
	s.Equal(100, beyond.Data.Page)
	for _, q := range []string{"?page=0", "?page=-1", "?page=abc", "?page_size=0", "?page_size=101", "?page_size=abc", "?page=9223372036854775807&page_size=100"} {
		s.http.expect(t, "GET", path+q, "", apiKey, "", 400)
	}
}
