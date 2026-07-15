package api

import (
	"errors"
	"fmt"
	"net/http"
	"testing"
)

func TestIsNotFound(t *testing.T) {
	if IsNotFound(nil) {
		t.Fatal("nil should not be not-found")
	}
	if IsNotFound(fmt.Errorf("other")) {
		t.Fatal("plain error should not be not-found")
	}
	if !IsNotFound(&APIError{Status: http.StatusNotFound, Message: "missing"}) {
		t.Fatal("404 APIError should be not-found")
	}
	if IsNotFound(&APIError{Status: http.StatusBadRequest}) {
		t.Fatal("400 should not be not-found")
	}
	// wrapped
	err := fmt.Errorf("wrap: %w", &APIError{Status: http.StatusNotFound})
	if !IsNotFound(err) {
		t.Fatal("wrapped 404 should be not-found")
	}
	if !errors.As(err, new(*APIError)) {
		t.Fatal("errors.As should find APIError")
	}
}

func TestIsNotImplemented(t *testing.T) {
	if !IsNotImplemented(&APIError{Status: http.StatusNotFound}) {
		t.Fatal("404 should count as not implemented")
	}
	if !IsNotImplemented(&APIError{Status: http.StatusNotImplemented}) {
		t.Fatal("501 should count as not implemented")
	}
	if IsNotImplemented(&APIError{Status: http.StatusForbidden}) {
		t.Fatal("403 should not be not-implemented")
	}
}
