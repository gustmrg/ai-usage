package provider

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"

	"github.com/gustmrg/ai-usage/internal/model"
)

func CacheFingerprint(value string) string {
	digest := sha256.Sum256([]byte(value))
	return hex.EncodeToString(digest[:8])
}

type Detection struct {
	Available bool
	Detail    string
}

type Provider interface {
	ID() string
	DisplayName() string
	Detect() Detection
	CacheKey() (string, error)
	Fetch(context.Context, string) (model.Snapshot, error)
}

type ErrorKind string

const (
	ErrorCredentials ErrorKind = "credentials"
	ErrorTransport   ErrorKind = "transport"
	ErrorHTTP        ErrorKind = "http"
	ErrorSchema      ErrorKind = "schema"
	ErrorCache       ErrorKind = "cache"
)

type Error struct {
	Kind       ErrorKind
	Provider   string
	StatusCode int
	Message    string
	Err        error
}

func (e *Error) Error() string {
	if e.Message != "" {
		return e.Message
	}
	if e.Err != nil {
		return e.Err.Error()
	}
	return fmt.Sprintf("%s provider error", e.Provider)
}

func (e *Error) Unwrap() error { return e.Err }

func Kind(err error) ErrorKind {
	var providerErr *Error
	if errors.As(err, &providerErr) {
		return providerErr.Kind
	}
	return ErrorTransport
}

func IsTransient(err error) bool {
	var providerErr *Error
	if !errors.As(err, &providerErr) {
		return true
	}
	return providerErr.Kind == ErrorTransport || providerErr.StatusCode == 429 || providerErr.StatusCode >= 500
}
