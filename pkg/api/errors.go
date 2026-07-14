package api

import "fmt"

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
