package api

import (
	"errors"
	"fmt"
	"net/http"
)

// APIError is a client-side API error.
type APIError struct {
	Status  int
	Code    string
	Message string
}

func (e *APIError) Error() string {
	if e.Code != "" {
		return fmt.Sprintf("api error %d (%s): %s", e.Status, e.Code, e.Message)
	}
	return fmt.Sprintf("api error %d: %s", e.Status, e.Message)
}

// IsNotFound reports whether err is an HTTP 404 APIError.
func IsNotFound(err error) bool {
	var ae *APIError
	return errors.As(err, &ae) && ae != nil && ae.Status == http.StatusNotFound
}

// IsNotImplemented reports whether err is HTTP 404 or 501 (endpoint missing/unimplemented).
func IsNotImplemented(err error) bool {
	var ae *APIError
	if !errors.As(err, &ae) || ae == nil {
		return false
	}
	return ae.Status == http.StatusNotFound || ae.Status == http.StatusNotImplemented
}

// ErrorBody is the standard error envelope.
type ErrorBody struct {
	Error ErrorDetail `json:"error"`
}

// ErrorDetail carries machine-readable error info.
type ErrorDetail struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Details any    `json:"details,omitempty"`
}
