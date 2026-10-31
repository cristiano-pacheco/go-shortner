package mapper

import (
	"github.com/cristiano-pacheco/go-shortner/internal/modules/shortener/http/dto"
	"github.com/cristiano-pacheco/go-shortner/internal/modules/shortener/usecase"
)

func ToLinkListResponse(output usecase.LinkListOutput) dto.LinkListResponse {
	items := make([]dto.LinkResponse, 0, len(output.Items))
	for _, item := range output.Items {
		items = append(items, dto.LinkResponse{
			ID: item.ID, Code: item.Code, Destination: item.Destination, ShortURL: item.ShortURL,
			Active: item.Active, ExpiresAt: item.ExpiresAt, CreatedAt: item.CreatedAt, UpdatedAt: item.UpdatedAt,
		})
	}
	return dto.LinkListResponse{Items: items, Page: output.Page, PageSize: output.PageSize, Total: output.Total}
}
