package httpclient

import (
	"encoding/json"
	"fmt"
)

// APIError represents an error response from the server.
type APIError struct {
	StatusCode int    `json:"-"`
	Code       string `json:"code"`
	Message    string `json:"message"`
	Detail     string `json:"detail,omitempty"`
}

func (e *APIError) Error() string {
	if e.Detail != "" {
		return fmt.Sprintf("httpclient: %d %s: %s (%s)", e.StatusCode, e.Code, e.Message, e.Detail)
	}
	return fmt.Sprintf("httpclient: %d %s: %s", e.StatusCode, e.Code, e.Message)
}

func parseAPIError(statusCode int, body []byte) *APIError {
	apiErr := &APIError{StatusCode: statusCode}
	if err := json.Unmarshal(body, apiErr); err != nil || apiErr.Code == "" {
		apiErr.Code = "UNKNOWN"
		apiErr.Message = string(body)
	}
	return apiErr
}
