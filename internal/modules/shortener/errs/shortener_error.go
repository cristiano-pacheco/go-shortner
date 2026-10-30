package errs

import "errors"

var (
	ErrLinkNotFound = errors.New("link not found")
	ErrCodeConflict = errors.New("code already reserved")
)
