package httpserver

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	brickserrs "github.com/cristiano-pacheco/bricks/pkg/errs"
)

// ReadJSON decodes one JSON value from the body bounded by the server middleware.
// It follows Bricks' media-type and error-status conventions, but requires EOF
// after the value, including after any trailing whitespace.
func ReadJSON(r *http.Request, dst any) error {
	header := r.Header.Get("Content-Type")
	contentType, _, _ := strings.Cut(header, ";")
	if header != "" && strings.TrimSpace(contentType) != "application/json" {
		return brickserrs.New("UNSUPPORTED_MEDIA_TYPE", "Content-Type header is not application/json", http.StatusUnsupportedMediaType, nil)
	}
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(dst); err != nil {
		return jsonRequestError(err)
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return jsonRequestError(err)
	}
	return nil
}

func jsonRequestError(err error) error {
	var maxBytes *http.MaxBytesError
	if errors.As(err, &maxBytes) {
		return brickserrs.New("REQUEST_ENTITY_TOO_LARGE", "Request body is too large", http.StatusRequestEntityTooLarge, nil)
	}
	return brickserrs.New("BAD_REQUEST", "Invalid JSON request body", http.StatusBadRequest, nil)
}
