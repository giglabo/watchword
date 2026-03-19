package domain

import "errors"

var (
	ErrNotFound               = errors.New("entry not found")
	ErrAlreadyActive          = errors.New("entry is already active")
	ErrAlreadyExpired         = errors.New("entry is already expired")
	ErrCollisionLimitExceeded = errors.New("could not resolve word collision after 999 attempts")
	ErrInvalidWord            = errors.New("word must be 1-500 characters, no leading/trailing spaces or control characters")
	ErrInvalidUUID            = errors.New("invalid UUID format")
	ErrUnauthorized           = errors.New("unauthorized: invalid or missing auth token")
	ErrPayloadTooLarge        = errors.New("payload exceeds maximum size limit (1MB)")
	ErrPayloadEmpty           = errors.New("payload must not be empty")
	ErrInvalidPattern         = errors.New("invalid search pattern")
	ErrInvalidTTL             = errors.New("ttl_hours must be between 0 and 8760")
	ErrInvalidFilename        = errors.New("invalid filename: must be 1-255 characters, no control characters or path separators")
	ErrNotAFileEntry          = errors.New("this entry is not a file entry; use get_entry or get_entry_by_word instead")
	ErrS3NotConfigured        = errors.New("S3 storage is not configured on this server")
)
