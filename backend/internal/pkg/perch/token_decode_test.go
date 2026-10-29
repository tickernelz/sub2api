package perch

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/andybalholm/brotli"
)

func encodeCompressed(t *testing.T, encoding string, payload []byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	switch encoding {
	case "gzip":
		writer := gzip.NewWriter(&buf)
		if _, err := writer.Write(payload); err != nil {
			t.Fatalf("gzip write: %v", err)
		}
		if err := writer.Close(); err != nil {
			t.Fatalf("gzip close: %v", err)
		}
	case "br":
		writer := brotli.NewWriter(&buf)
		if _, err := writer.Write(payload); err != nil {
			t.Fatalf("brotli write: %v", err)
		}
		if err := writer.Close(); err != nil {
			t.Fatalf("brotli close: %v", err)
		}
	default:
		t.Fatalf("unknown encoding %q", encoding)
	}
	return buf.Bytes()
}

func newCompressedTokenServer(t *testing.T, encoding string) *httptest.Server {
	t.Helper()
	token := encodeCompressed(t, encoding, []byte(`{"access_token":"access-after-refresh","token_type":"bearer","expires_in":3600,"refresh_token":"refresh-after-rotate","user":{"id":"11111111-1111-4111-8111-111111111111","email":"sample@example.com"}}`))
	mux := http.NewServeMux()
	mux.HandleFunc(defaultAuthConfigPath, func(w http.ResponseWriter, r *http.Request) {
		payload, err := json.Marshal(map[string]any{
			"ok":              true,
			"appUrl":          "http://" + r.Host,
			"supabaseUrl":     "http://" + r.Host,
			"supabaseAnonKey": "sb_publishable_test",
			"providers":       []string{"google", "github"},
			"redirectHost":    "127.0.0.1",
		})
		if err != nil {
			t.Errorf("encode config: %v", err)
			return
		}
		w.Header().Set("content-type", "application/json")
		_, _ = w.Write(payload)
	})
	mux.HandleFunc(tokenPath, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("content-type", "application/json")
		w.Header().Set("content-encoding", encoding)
		_, _ = w.Write(token)
	})
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)
	return server
}

func TestRefreshDecodesCompressedTokenResponse(t *testing.T) {
	for _, encoding := range []string{"gzip", "br"} {
		t.Run(encoding, func(t *testing.T) {
			server := newCompressedTokenServer(t, encoding)
			session := NewSession(
				Credentials{AppURL: server.URL, RefreshToken: "refresh-token"},
				WithHTTPClient(server.Client()),
			)

			token, err := session.Refresh(context.Background())
			if err != nil {
				t.Fatalf("refresh with %s response: %v", encoding, err)
			}
			if token != "access-after-refresh" {
				t.Fatalf("token = %q", token)
			}
			if rotated := session.Credentials().RefreshToken; rotated != "refresh-after-rotate" {
				t.Fatalf("refresh token = %q", rotated)
			}
		})
	}
}

func TestPKCEExchangeDecodesCompressedTokenResponse(t *testing.T) {
	for _, encoding := range []string{"gzip", "br"} {
		t.Run(encoding, func(t *testing.T) {
			server := newCompressedTokenServer(t, encoding)
			client := NewLoginClient(WithHTTPClient(server.Client()))

			creds, err := client.Exchange(context.Background(), LoginFlow{
				SupabaseURL:     server.URL,
				SupabaseAnonKey: "sb_publishable_test",
				CodeVerifier:    strings.Repeat("v", 112),
				RedirectURI:     "http://127.0.0.1:62173/perch/callback",
			}, "auth-code")
			if err != nil {
				t.Fatalf("exchange with %s response: %v", encoding, err)
			}
			if creds.AccessToken != "access-after-refresh" {
				t.Fatalf("access token = %q", creds.AccessToken)
			}
		})
	}
}
