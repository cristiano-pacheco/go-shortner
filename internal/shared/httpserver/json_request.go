package httpserver

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	sharederrs "github.com/cristiano-pacheco/go-shortner/internal/shared/errs"
)

// ReadJSON decodes one JSON value from the body bounded by the server middleware.
// It follows Bricks' media-type and error-status conventions, but requires EOF
// after the value, including after any trailing whitespace.
func ReadJSON(r *http.Request, dst any) error {
	header := r.Header.Get("Content-Type")

	contentType, _, _ := strings.Cut(header, ";")
	if header != "" && strings.TrimSpace(contentType) != "application/json" {
		return sharederrs.ErrUnsupportedMediaType
	}

	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()

	if err := decoder.Decode(dst); err != nil {
		return jsonRequestError(err)
	}

	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		if _, ok := errors.AsType[*http.MaxBytesError](err); ok {
			return sharederrs.ErrRequestTooLarge
		}

		return sharederrs.ErrTrailingJSON
	}

	return nil
}

func jsonRequestError(err error) error {
	if _, ok := errors.AsType[*http.MaxBytesError](err); ok {
		return sharederrs.ErrRequestTooLarge
	}

	if errors.Is(err, io.EOF) {
		return sharederrs.ErrEmptyJSONBody
	}

	if _, ok := errors.AsType[*json.UnmarshalTypeError](err); ok {
		return fmt.Errorf("%w: %w", sharederrs.ErrInvalidJSONFieldType, err)
	}

	if _, ok := errors.AsType[*time.ParseError](err); ok {
		return sharederrs.ErrInvalidJSONTime
	}

	if strings.HasPrefix(err.Error(), "Time.UnmarshalJSON:") {
		return sharederrs.ErrInvalidJSONTime
	}

	if strings.HasPrefix(err.Error(), "json: unknown field ") {
		return sharederrs.ErrUnknownJSONField
	}

	return sharederrs.ErrMalformedJSON
}
