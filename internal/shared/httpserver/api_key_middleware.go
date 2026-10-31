package httpserver

import (
	"crypto/sha256"
	"crypto/subtle"
	"net/http"

	brickserrs "github.com/cristiano-pacheco/bricks/pkg/errs"
	"github.com/cristiano-pacheco/bricks/pkg/http/response"
	"github.com/cristiano-pacheco/go-shortner/internal/shared/config"
)

var errUnauthenticated = brickserrs.New(
	"unauthenticated",
	"API key is missing or invalid",
	http.StatusUnauthorized,
	nil,
)

// APIKeyMiddleware protects management routes with the configured global key.
type APIKeyMiddleware struct {
	keyHash [sha256.Size]byte
	errors  response.ErrorHandler
}

func NewAPIKeyMiddleware(cfg config.Config, errors response.ErrorHandler) *APIKeyMiddleware {
	return &APIKeyMiddleware{keyHash: sha256.Sum256([]byte(cfg.APIKey)), errors: errors}
}

func (m *APIKeyMiddleware) Authenticate(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		keys := r.Header.Values("X-Api-Key")
		if len(keys) != 1 || keys[0] == "" {
			m.errors.Error(w, errUnauthenticated)
			return
		}
		keyHash := sha256.Sum256([]byte(keys[0]))
		if subtle.ConstantTimeCompare(keyHash[:], m.keyHash[:]) != 1 {
			m.errors.Error(w, errUnauthenticated)
			return
		}
		next.ServeHTTP(w, r)
	})
}
