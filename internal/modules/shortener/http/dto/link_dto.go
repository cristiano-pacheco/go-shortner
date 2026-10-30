package dto

import "time"

type LinkCreateRequest struct {
	Destination string     `json:"destination"`
	Alias       *string    `json:"alias"`
	ExpiresAt   *time.Time `json:"expires_at"`
}

type LinkResponse struct {
	ID          int64      `json:"id"`
	Code        string     `json:"code"`
	Destination string     `json:"destination"`
	ShortURL    string     `json:"short_url"`
	Active      bool       `json:"active"`
	ExpiresAt   *time.Time `json:"expires_at"`
	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
}
