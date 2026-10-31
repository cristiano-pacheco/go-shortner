package mapper_test

import (
	"testing"
	"time"

	"github.com/cristiano-pacheco/go-shortner/internal/modules/shortener/http/dto"
	"github.com/cristiano-pacheco/go-shortner/internal/modules/shortener/http/mapper"
	"github.com/cristiano-pacheco/go-shortner/internal/modules/shortener/usecase"
	"github.com/stretchr/testify/suite"
)

type LinkListMapperSuite struct {
	suite.Suite
}

func TestLinkListMapperSuite(t *testing.T) {
	suite.Run(t, new(LinkListMapperSuite))
}

func (s *LinkListMapperSuite) TestMapsItemsAndPagination() {
	created := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	updated := created.Add(time.Hour)
	expires := created.Add(24 * time.Hour)
	output := usecase.LinkListOutput{
		Items: []usecase.LinkListItem{
			{ID: 1, Code: "abc", Destination: "https://example.com", ShortURL: "https://short/abc", Active: true, ExpiresAt: &expires, CreatedAt: created, UpdatedAt: updated},
			{ID: 2, Code: "def", Destination: "https://example.org", ShortURL: "https://short/def", Active: false, CreatedAt: updated, UpdatedAt: created},
		},
		Page: 2, PageSize: 10, Total: 12,
	}

	response := mapper.ToLinkListResponse(output)

	s.Equal(dto.LinkListResponse{
		Items: []dto.LinkResponse{
			{ID: 1, Code: "abc", Destination: "https://example.com", ShortURL: "https://short/abc", Active: true, ExpiresAt: &expires, CreatedAt: created, UpdatedAt: updated},
			{ID: 2, Code: "def", Destination: "https://example.org", ShortURL: "https://short/def", Active: false, CreatedAt: updated, UpdatedAt: created},
		},
		Page: 2, PageSize: 10, Total: 12,
	}, response)
}

func (s *LinkListMapperSuite) TestEmptyAndNilItemsBecomeEmptySlice() {
	for _, items := range [][]usecase.LinkListItem{nil, {}} {
		response := mapper.ToLinkListResponse(usecase.LinkListOutput{Items: items})
		s.NotNil(response.Items)
		s.Empty(response.Items)
	}
}
