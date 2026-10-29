package perch

import (
	"net/http"
	"strings"
	"testing"
)

func TestWireDefaultAcceptEncodingCarriesBrotli(t *testing.T) {
	for _, target := range []string{"http://perch.test/api/perchai/account", "https://perch.test/api/perchai/account"} {
		req, err := http.NewRequest(http.MethodGet, target, nil)
		if err != nil {
			t.Fatalf("request %s: %v", target, err)
		}
		payload, err := buildWireRequest(req, nil)
		if err != nil {
			t.Fatalf("build %s: %v", target, err)
		}
		if !strings.Contains(string(payload), "accept-encoding: br, gzip, deflate\r\n") {
			t.Fatalf("%s does not send the recorded CLI accept-encoding:\n%s", target, payload)
		}
	}
}
