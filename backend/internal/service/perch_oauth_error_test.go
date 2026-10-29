package service

import (
	"errors"
	"net/http"
	"testing"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/pkg/perch"
)

func TestPerchOAuthErrorMapping(t *testing.T) {
	cases := []struct {
		name       string
		err        error
		wantStatus int
		wantReason string
	}{
		{"unauthorized", &perch.APIError{Status: http.StatusUnauthorized, Code: "invalid_token", Message: "session rejected"}, http.StatusBadGateway, "PERCH_OAUTH_INVALID_TOKEN"},
		{"forbidden", &perch.APIError{Status: http.StatusForbidden, Code: "not_allowed", Message: "no"}, http.StatusBadGateway, "PERCH_OAUTH_NOT_ALLOWED"},
		{"upstream 503", &perch.APIError{Status: http.StatusServiceUnavailable, Code: "unavailable", Message: "down"}, http.StatusBadGateway, "PERCH_OAUTH_UNAVAILABLE"},
		{"used code", &perch.APIError{Status: http.StatusNotFound, Code: "invalid flow state", Message: "no valid flow state"}, http.StatusNotFound, "PERCH_OAUTH_INVALID_FLOW_STATE"},
		{"bad request", &perch.APIError{Status: http.StatusBadRequest, Code: "invalid_grant", Message: "expired"}, http.StatusBadRequest, "PERCH_OAUTH_INVALID_GRANT"},
		{"transport", errors.New("dial refused"), http.StatusBadGateway, "PERCH_OAUTH_FALLBACK"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			mapped := perchOAuthError(tc.err, "PERCH_OAUTH_FALLBACK")
			if got := infraerrors.Code(mapped); got != tc.wantStatus {
				t.Fatalf("status = %d, want %d (%v)", got, tc.wantStatus, mapped)
			}
			if got := infraerrors.Reason(mapped); got != tc.wantReason {
				t.Fatalf("reason = %q, want %q", got, tc.wantReason)
			}
		})
	}
}
