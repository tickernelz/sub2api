package perch

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
)

const (
	CodeNotAuthenticated      = "not_authenticated"
	CodeUsageLimitReached     = "usage_limit_reached"
	CodeTurnRateLimited       = "turn_rate_limited"
	CodePlanRequired          = "plan_required"
	CodeModelBlocked          = "model_blocked"
	CodePromoOverflow         = "promo_overflow_decision"
	CodeAPIError              = "api_error"
	CodeProviderNotConfigured = "provider_not_configured"
)

// Limit is the quota detail Perch attaches to a limit error, passed through untouched.
type Limit struct {
	Raw json.RawMessage
}

func (l *Limit) UnmarshalJSON(data []byte) error {
	if l == nil {
		return nil
	}
	l.Raw = append(l.Raw[:0], data...)
	return nil
}

func (l Limit) MarshalJSON() ([]byte, error) {
	if len(l.Raw) == 0 {
		return []byte("null"), nil
	}
	return l.Raw, nil
}

func (l *Limit) Empty() bool {
	return l == nil || len(l.Raw) == 0 || string(l.Raw) == "null"
}

// APIError is a Perch failure with the status and code callers map to OpenAI errors.
type APIError struct {
	Status  int
	Code    string
	Message string
	Limit   *Limit
}

func (e *APIError) Error() string {
	if e == nil {
		return "perch: unknown error"
	}
	code := e.Code
	if code == "" {
		code = StatusToCode(e.Status)
	}
	return fmt.Sprintf("perch: %s (status %d): %s", code, e.Status, e.Message)
}

func isSessionCode(code string) bool {
	switch strings.ToLower(strings.TrimSpace(code)) {
	case "invalid_grant", "invalid_refresh_token", "refresh_token_not_found", "session_expired", "session_not_found", "invalid_jwt":
		return true
	}
	return false
}

// HTTPStatus is the status a client should receive for this failure.
func (e *APIError) HTTPStatus() int {
	if e == nil {
		return http.StatusBadGateway
	}
	if isSessionCode(e.Code) {
		return http.StatusUnauthorized
	}
	switch {
	case e.Status >= 400 && e.Status <= 599:
		return e.Status
	case e.Status > 0:
		return http.StatusBadGateway
	default:
		return http.StatusBadGateway
	}
}

// OpenAIType maps the Perch code onto an OpenAI error type.
func (e *APIError) OpenAIType() string {
	if e == nil {
		return "api_error"
	}
	switch e.Code {
	case CodeNotAuthenticated:
		return "authentication_error"
	case CodeUsageLimitReached, CodePlanRequired:
		return "insufficient_quota"
	case CodeTurnRateLimited:
		return "rate_limit_error"
	case CodeModelBlocked:
		return "invalid_request_error"
	}
	switch e.HTTPStatus() {
	case http.StatusUnauthorized:
		return "authentication_error"
	case http.StatusForbidden:
		return "permission_error"
	case http.StatusNotFound:
		return "not_found_error"
	case http.StatusTooManyRequests:
		return "rate_limit_error"
	case http.StatusBadRequest, http.StatusUnprocessableEntity:
		return "invalid_request_error"
	}
	return "api_error"
}

// StatusToCode converts a bare HTTP status into a Perch-style code.
func StatusToCode(status int) string {
	switch status {
	case http.StatusUnauthorized:
		return CodeNotAuthenticated
	case http.StatusForbidden:
		return "forbidden"
	case http.StatusNotFound:
		return "not_found"
	case http.StatusRequestTimeout:
		return "timeout"
	case http.StatusConflict:
		return "conflict"
	case http.StatusTooManyRequests:
		return CodeUsageLimitReached
	}
	if status >= 500 {
		return CodeAPIError
	}
	return CodeAPIError
}

// NewAPIError builds an error from a decoded Perch error envelope.
func NewAPIError(status int, payload []byte) *APIError {
	var parsed struct {
		OK        *bool  `json:"ok"`
		Error     string `json:"error"`
		ErrorCode string `json:"errorCode"`
		Limit     *Limit `json:"limit"`
	}
	err := &APIError{Status: status}
	if json.Unmarshal(payload, &parsed) != nil {
		err.Message = truncateBody(payload)
		err.Code = StatusToCode(status)
		return err
	}
	err.Message = strings.TrimSpace(parsed.Error)
	err.Code = strings.TrimSpace(parsed.ErrorCode)
	if err.Code == "" {
		err.Code = StatusToCode(status)
	}
	if err.Message == "" {
		err.Message = truncateBody(payload)
	}
	if !parsed.Limit.Empty() {
		err.Limit = parsed.Limit
	}
	return err
}

// IsAuthError reports whether the failure is a rejected or expired session.
func IsAuthError(err error) bool {
	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		return false
	}
	return apiErr.HTTPStatus() == http.StatusUnauthorized || apiErr.Code == CodeNotAuthenticated
}
