// Package testhelpers provides testing utilities for mocking HTTP requests and responses.
package testhelpers

import (
	"bytes"
	"context"
	"io"
	"net/http"

	"github.com/cockroachdb/errors"
)

// MockRoundTripper intercepts HTTP requests and returns mock responses
type MockRoundTripper struct {
	ResponseFunc func(*http.Request) (*http.Response, error)
}

func (m *MockRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	return m.ResponseFunc(req)
}

// NewMockHTTPClient creates http.Client with mock transport
func NewMockHTTPClient(responseFunc func(*http.Request) (*http.Response, error)) *http.Client {
	return &http.Client{
		Transport: &MockRoundTripper{ResponseFunc: responseFunc},
	}
}

// MockSuccessResponse returns 200 OK with body
func MockSuccessResponse(body []byte) (*http.Response, error) {
	return &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(bytes.NewReader(body)),
		Header:     make(http.Header),
	}, nil
}

// MockStatusResponse returns custom status code
func MockStatusResponse(status int) (*http.Response, error) {
	return &http.Response{
		StatusCode: status,
		Body:       io.NopCloser(bytes.NewReader([]byte{})),
		Header:     make(http.Header),
	}, nil
}

// MockTimeoutError simulates timeout
func MockTimeoutError() (*http.Response, error) {
	return nil, context.DeadlineExceeded
}

// MockNetworkError simulates network failure
func MockNetworkError() (*http.Response, error) {
	return nil, errors.New("connection refused")
}
