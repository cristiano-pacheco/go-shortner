//go:build e2e

package shortner_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/suite"
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

//nolint:paralleltest // Management suites share a Compose stack.
func TestListLinkSuite(t *testing.T) {
	suite.Run(t, new(ListLinkSuite))
}

func (s *ListLinkSuite) list(query string) listResponse {
	s.T().Helper()
	_, body := s.http.expect(s.T(), http.MethodGet, "/api/v1/links"+query, "", apiKey, "", http.StatusOK)

	var response listResponse
	s.Require().NoError(json.Unmarshal(body, &response))

	return response
}

func (s *ListLinkSuite) seedLinks(count int) {
	s.T().Helper()
	s.sql(fmt.Sprintf(`INSERT INTO links (code, destination, active, created_at, updated_at)
SELECT 'list-' || n, 'https://destination.example/' || n, true,
 '2021-01-01 00:00:00+00'::timestamptz + ((n - 1) / 2) * interval '1 second',
 '2021-02-01 00:00:00+00'::timestamptz
FROM generate_series(1, %d) AS n ORDER BY n`, count))
}

func (s *ListLinkSuite) TestMissingAuthentication() {
	s.http.expect(s.T(), http.MethodGet, "/api/v1/links", "", "", "", http.StatusUnauthorized)
}

func (s *ListLinkSuite) TestInvalidAuthentication() {
	s.http.expect(s.T(), http.MethodGet, "/api/v1/links", "", "wrong", "", http.StatusUnauthorized)
}

func (s *ListLinkSuite) TestEmptyListUsesDefaults() {
	response := s.list("")
	s.NotNil(response.Data.Items)
	s.Empty(response.Data.Items)
	s.Equal(0, response.Data.Total)
	s.Equal(1, response.Data.Page)
	s.Equal(20, response.Data.PageSize)
}

func (s *ListLinkSuite) TestOnlyDeletedLinksProduceEmptyList() {
	s.seedLinks(3)
	s.sql(`UPDATE links SET deleted_at = '2021-03-01 00:00:00+00'`)
	response := s.list("?page=1&page_size=2")
	s.NotNil(response.Data.Items)
	s.Empty(response.Data.Items)
	s.Equal(0, response.Data.Total)
	s.Equal(1, response.Data.Page)
	s.Equal(2, response.Data.PageSize)
}

func (s *ListLinkSuite) TestUnknownQueryParameterDoesNotChangePagination() {
	s.seedLinks(3)
	response := s.list("?ignored=value&page_size=2")
	s.Equal(1, response.Data.Page)
	s.Equal(2, response.Data.PageSize)
	s.Equal(3, response.Data.Total)
	s.Require().Len(response.Data.Items, 2)
	s.Equal([]int64{3, 2}, []int64{response.Data.Items[0].ID, response.Data.Items[1].ID})
}

func (s *ListLinkSuite) TestDefaultPaginationAcrossPages() {
	s.seedLinks(25)
	first := s.list("")
	second := s.list("?page=2")

	s.Equal(25, first.Data.Total)
	s.Equal(25, second.Data.Total)
	s.Equal(1, first.Data.Page)
	s.Equal(2, second.Data.Page)
	s.Equal(20, first.Data.PageSize)
	s.Equal(20, second.Data.PageSize)
	s.Require().Len(first.Data.Items, 20)
	s.Require().Len(second.Data.Items, 5)

	for index, item := range append(first.Data.Items, second.Data.Items...) {
		s.Equal(int64(25-index), item.ID)
	}
}

func (s *ListLinkSuite) assertCustomPage(page int, want []int64) {
	s.T().Helper()
	s.seedLinks(7)
	response := s.list(fmt.Sprintf("?page=%d&page_size=3", page))
	s.Equal(page, response.Data.Page)
	s.Equal(3, response.Data.PageSize)
	s.Equal(7, response.Data.Total)
	s.NotNil(response.Data.Items)

	ids := make([]int64, 0, len(response.Data.Items))
	for _, item := range response.Data.Items {
		ids = append(ids, item.ID)
	}

	s.Equal(want, ids)
}

func (s *ListLinkSuite) TestCustomFirstPage() {
	s.assertCustomPage(1, []int64{7, 6, 5})
}

func (s *ListLinkSuite) TestCustomMiddlePage() {
	s.assertCustomPage(2, []int64{4, 3, 2})
}

func (s *ListLinkSuite) TestCustomPartialLastPage() {
	s.assertCustomPage(3, []int64{1})
}

func (s *ListLinkSuite) TestCustomPageBeyondLast() {
	s.assertCustomPage(4, []int64{})
}

func (s *ListLinkSuite) TestExactPageBoundary() {
	s.seedLinks(6)
	last := s.list("?page=2&page_size=3")
	s.Equal(6, last.Data.Total)
	s.Require().Len(last.Data.Items, 3)
	s.Equal([]int64{3, 2, 1}, []int64{last.Data.Items[0].ID, last.Data.Items[1].ID, last.Data.Items[2].ID})

	beyond := s.list("?page=3&page_size=3")
	s.NotNil(beyond.Data.Items)
	s.Empty(beyond.Data.Items)
	s.Equal(6, beyond.Data.Total)
	s.Equal(3, beyond.Data.Page)
	s.Equal(3, beyond.Data.PageSize)
}

func (s *ListLinkSuite) TestPageSizeBoundaries() {
	s.seedLinks(101)
	first := s.list("?page_size=100")
	s.Equal(1, first.Data.Page)
	s.Equal(100, first.Data.PageSize)
	s.Equal(101, first.Data.Total)
	s.Require().Len(first.Data.Items, 100)
	s.Equal(int64(101), first.Data.Items[0].ID)
	s.Equal(int64(2), first.Data.Items[99].ID)

	last := s.list("?page=2&page_size=100")
	s.Require().Len(last.Data.Items, 1)
	s.Equal(int64(1), last.Data.Items[0].ID)
	s.Equal(101, last.Data.Total)

	single := s.list("?page=2&page_size=1")
	s.Require().Len(single.Data.Items, 1)
	s.Equal(int64(100), single.Data.Items[0].ID)
	s.Equal(1, single.Data.PageSize)
	s.Equal(101, single.Data.Total)
}

func (s *ListLinkSuite) TestPageDefaultsIndependently() {
	s.seedLinks(21)
	pageOnly := s.list("?page=2")
	s.Equal(2, pageOnly.Data.Page)
	s.Equal(20, pageOnly.Data.PageSize)
	s.Require().Len(pageOnly.Data.Items, 1)
	s.Equal(int64(1), pageOnly.Data.Items[0].ID)

	sizeOnly := s.list("?page_size=2")
	s.Equal(1, sizeOnly.Data.Page)
	s.Equal(2, sizeOnly.Data.PageSize)
	s.Require().Len(sizeOnly.Data.Items, 2)
	s.Equal([]int64{21, 20}, []int64{sizeOnly.Data.Items[0].ID, sizeOnly.Data.Items[1].ID})
}

func (s *ListLinkSuite) TestOrderingByCreationTimeAndID() {
	s.seedLinks(6)
	// Override insertion order to distinguish creation-time ordering from ID ordering.
	s.sql(`UPDATE links SET created_at = '2022-01-01 00:00:00+00' WHERE id = 1`)
	response := s.list("?page_size=10")
	s.Require().Len(response.Data.Items, 6)

	ids := make([]int64, 0, 6)
	for _, item := range response.Data.Items {
		ids = append(ids, item.ID)
	}

	s.Equal([]int64{1, 6, 5, 4, 3, 2}, ids)
}

func (s *ListLinkSuite) TestLinkFieldsAndVisibility() {
	s.seedLinks(4)
	s.sql(`UPDATE links SET active = false WHERE id = 1;
UPDATE links SET expires_at = '2020-01-01 00:00:00+00' WHERE id = 2;
UPDATE links SET deleted_at = '2021-03-01 00:00:00+00' WHERE id = 4`)
	response := s.list("?page_size=10")
	s.Equal(3, response.Data.Total)
	s.Require().Len(response.Data.Items, 3)

	for index, item := range response.Data.Items {
		id := int64(3 - index)
		s.Equal(id, item.ID)
		s.Equal(fmt.Sprintf("list-%d", id), item.Code)
		s.Equal(fmt.Sprintf("https://destination.example/%d", id), item.Destination)
		s.Equal(fmt.Sprintf("%s/list-%d", publicBase, id), item.ShortURL)
		s.Equal(id != 1, item.Active)
		s.Equal(time.Date(2021, 1, 1, 0, 0, int((id-1)/2), 0, time.UTC), item.CreatedAt)
		s.Equal(time.Date(2021, 2, 1, 0, 0, 0, 0, time.UTC), item.UpdatedAt)

		if id == 2 {
			if s.NotNil(item.ExpiresAt) {
				s.True(item.ExpiresAt.Equal(time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)))
			}
		} else {
			s.Nil(item.ExpiresAt)
		}
	}
}

func (s *ListLinkSuite) TestDeletedLinksDoNotAffectPaginationOrTotal() {
	s.seedLinks(5)
	s.sql(`UPDATE links SET deleted_at = '2021-03-01 00:00:00+00' WHERE id IN (2, 4)`)
	first := s.list("?page=1&page_size=2")
	s.Equal(3, first.Data.Total)
	s.Require().Len(first.Data.Items, 2)
	s.Equal([]int64{5, 3}, []int64{first.Data.Items[0].ID, first.Data.Items[1].ID})
	second := s.list("?page=2&page_size=2")
	s.Equal(3, second.Data.Total)
	s.Require().Len(second.Data.Items, 1)
	s.Equal(int64(1), second.Data.Items[0].ID)
}

func (s *ListLinkSuite) assertInvalidQuery(query string) {
	s.T().Helper()
	s.http.expect(s.T(), http.MethodGet, "/api/v1/links"+query, "", apiKey, "", http.StatusBadRequest)
}

func (s *ListLinkSuite) TestZeroPage() {
	s.assertInvalidQuery("?page=0")
}

func (s *ListLinkSuite) TestNegativePage() {
	s.assertInvalidQuery("?page=-1")
}

func (s *ListLinkSuite) TestEmptyPage() {
	s.assertInvalidQuery("?page=")
}

func (s *ListLinkSuite) TestNonNumericPage() {
	s.assertInvalidQuery("?page=abc")
}

func (s *ListLinkSuite) TestFractionalPage() {
	s.assertInvalidQuery("?page=1.5")
}

func (s *ListLinkSuite) TestDuplicatePage() {
	s.assertInvalidQuery("?page=1&page=2")
}

func (s *ListLinkSuite) TestPageIntegerOverflow() {
	s.assertInvalidQuery("?page=9999999999999999999999999")
}

func (s *ListLinkSuite) TestZeroPageSize() {
	s.assertInvalidQuery("?page_size=0")
}

func (s *ListLinkSuite) TestNegativePageSize() {
	s.assertInvalidQuery("?page_size=-1")
}

func (s *ListLinkSuite) TestEmptyPageSize() {
	s.assertInvalidQuery("?page_size=")
}

func (s *ListLinkSuite) TestNonNumericPageSize() {
	s.assertInvalidQuery("?page_size=abc")
}

func (s *ListLinkSuite) TestFractionalPageSize() {
	s.assertInvalidQuery("?page_size=1.5")
}

func (s *ListLinkSuite) TestDuplicatePageSize() {
	s.assertInvalidQuery("?page_size=1&page_size=2")
}

func (s *ListLinkSuite) TestPageSizeAboveMaximum() {
	s.assertInvalidQuery("?page_size=101")
}

func (s *ListLinkSuite) TestPageSizeIntegerOverflow() {
	s.assertInvalidQuery("?page_size=9999999999999999999999999")
}

func (s *ListLinkSuite) TestOffsetOverflow() {
	s.assertInvalidQuery("?page=9223372036854775807&page_size=100")
}

func (s *ListLinkSuite) TestInvalidQueryEscape() {
	s.assertInvalidQuery("?page=%zz")
}
