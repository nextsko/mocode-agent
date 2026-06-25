package memory

import "errors"

var (
	// ErrAppNameRequired is returned when app name is missing.
	ErrAppNameRequired = errors.New("app name is required")
	// ErrUserIDRequired is returned when user ID is missing.
	ErrUserIDRequired = errors.New("user ID is required")
	// ErrMemoryIDRequired is returned when memory ID is missing.
	ErrMemoryIDRequired = errors.New("memory ID is required")
	// ErrMemoryRequired is returned when memory content is missing.
	ErrMemoryRequired = errors.New("memory content is required")
	// ErrMemoryNotFound is returned when a memory is not found.
	ErrMemoryNotFound = errors.New("memory not found")
	// ErrUserNotFound is returned when a user is not found.
	ErrUserNotFound = errors.New("user not found")
)
