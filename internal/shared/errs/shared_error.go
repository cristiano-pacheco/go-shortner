package errs

import (
	"net/http"

	brickserrs "github.com/cristiano-pacheco/bricks/pkg/errs"
)

var (
	ErrMigrationFailed           = brickserrs.New("SHORTENER_06", "database migration failed", http.StatusInternalServerError, nil)
	ErrServerStopped             = brickserrs.New("SHORTENER_07", "HTTP server stopped unexpectedly", http.StatusInternalServerError, nil)
	ErrDatabaseHostRequired      = brickserrs.New("SHORTENER_08", "database host is required", http.StatusInternalServerError, nil)
	ErrDatabasePortInvalid       = brickserrs.New("SHORTENER_09", "database port must be between 1 and 65535", http.StatusInternalServerError, nil)
	ErrDatabaseSSLModeInvalid    = brickserrs.New("SHORTENER_10", "invalid database SSL mode", http.StatusInternalServerError, nil)
	ErrAPIKeyRequired            = brickserrs.New("SHORTENER_11", "API key is required", http.StatusInternalServerError, nil)
	ErrPublicBaseURLInvalid      = brickserrs.New("SHORTENER_12", "public base URL must be an absolute HTTP or HTTPS URL", http.StatusInternalServerError, nil)
	ErrRedisURLRequired          = brickserrs.New("SHORTENER_13", "Redis URL is required", http.StatusInternalServerError, nil)
	ErrHTTPAddressInvalid        = brickserrs.New("SHORTENER_14", "HTTP address must include a port", http.StatusInternalServerError, nil)
	ErrPostgreSQLInit            = brickserrs.New("SHORTENER_15", "cannot initialize PostgreSQL", http.StatusInternalServerError, nil)
	ErrPostgreSQLPoolInit        = brickserrs.New("SHORTENER_16", "cannot initialize PostgreSQL pool", http.StatusInternalServerError, nil)
	ErrPostgreSQLConnect         = brickserrs.New("SHORTENER_17", "cannot connect to PostgreSQL", http.StatusInternalServerError, nil)
	ErrRedisURLInvalid           = brickserrs.New("SHORTENER_18", "invalid Redis URL", http.StatusInternalServerError, nil)
	ErrRedisConnect              = brickserrs.New("SHORTENER_19", "cannot connect to Redis", http.StatusInternalServerError, nil)
	ErrMigrationFilenameInvalid  = brickserrs.New("SHORTENER_20", "invalid migration filename", http.StatusInternalServerError, nil)
	ErrMigrationVersionDuplicate = brickserrs.New("SHORTENER_21", "duplicate migration version", http.StatusInternalServerError, nil)
	ErrMigrationRead             = brickserrs.New("SHORTENER_22", "read migration files", http.StatusInternalServerError, nil)
	ErrMigrationFileDuplicate    = brickserrs.New("SHORTENER_23", "duplicate migration file", http.StatusInternalServerError, nil)
	ErrMetricsPoolRead           = brickserrs.New("SHORTENER_24", "get PostgreSQL pool for metrics", http.StatusInternalServerError, nil)
	ErrMetricsPoolRegister       = brickserrs.New("SHORTENER_25", "register pool metrics", http.StatusInternalServerError, nil)
	ErrUnsupportedMediaType      = brickserrs.New("SHORTENER_26", "Content-Type header is not application/json", http.StatusUnsupportedMediaType, nil)
	ErrRequestTooLarge           = brickserrs.New("SHORTENER_27", "Request body is too large", http.StatusRequestEntityTooLarge, nil)
	ErrMalformedJSON             = brickserrs.New("SHORTENER_28", "request body contains malformed JSON", http.StatusBadRequest, nil)
	ErrUnauthenticated           = brickserrs.New("SHORTENER_29", "API key is missing or invalid", http.StatusUnauthorized, nil)
	ErrDatabaseNameRequired      = brickserrs.New("SHORTENER_52", "database name is required", http.StatusInternalServerError, nil)
	ErrDatabaseUserRequired      = brickserrs.New("SHORTENER_53", "database user is required", http.StatusInternalServerError, nil)
	ErrDatabasePasswordRequired  = brickserrs.New("SHORTENER_54", "database password is required", http.StatusInternalServerError, nil)
	ErrEmptyJSONBody             = brickserrs.New("SHORTENER_55", "request body must not be empty", http.StatusBadRequest, nil)
	ErrUnknownJSONField          = brickserrs.New("SHORTENER_56", "request body contains an unknown field", http.StatusBadRequest, nil)
	ErrInvalidJSONFieldType      = brickserrs.New("SHORTENER_57", "request body contains a field with an invalid type", http.StatusBadRequest, nil)
	ErrTrailingJSON              = brickserrs.New("SHORTENER_58", "request body must contain only one JSON value", http.StatusBadRequest, nil)
	ErrInvalidJSONTime           = brickserrs.New("SHORTENER_59", "request body contains an invalid timestamp", http.StatusBadRequest, nil)
)
