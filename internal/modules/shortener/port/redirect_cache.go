package port

import (
	"context"
	"time"
)

// RedirectSnapshot is a database observation, valid only until FreshUntil.
type RedirectSnapshot struct {
	Destination string     `json:"destination"`
	Active      bool       `json:"active"`
	ExpiresAt   *time.Time `json:"expires_at"`
	FreshUntil  time.Time  `json:"fresh_until"`
}

// RedirectCache stores positive redirect observations, not authoritative state.
type RedirectCache interface {
	Get(context.Context, string) (RedirectSnapshot, bool, error)
	Set(context.Context, string, RedirectSnapshot) error
	Delete(context.Context, string) error
}
