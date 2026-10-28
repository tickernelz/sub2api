package perch

import (
	"net/http"
	"os"
	"strings"

	"golang.org/x/mod/semver"
)

const CLIVersionEnv = "PERCH_CLI_VERSION"

const CLICurrentVersion = "2.4.120"

var resolvedCLIVersion = resolveCLIVersion(os.Getenv(CLIVersionEnv))

func CLIVersion() string {
	return resolvedCLIVersion
}

func IsSupportedCLIVersion(version string) bool {
	version = strings.TrimSpace(version)
	if version == "" {
		return false
	}
	canonical := "v" + version
	if !semver.IsValid(canonical) || semver.Canonical(canonical) != canonical {
		return false
	}
	if semver.Prerelease(canonical) != "" || semver.Build(canonical) != "" {
		return false
	}
	return true
}

func resolveCLIVersion(override string) string {
	if !IsSupportedCLIVersion(override) {
		return CLICurrentVersion
	}
	return strings.TrimSpace(override)
}

func DefaultUserAgent() string {
	return "perchai-cli/" + CLIVersion()
}

func DefaultHeaders() map[string]string {
	return map[string]string{
		"user-agent":         DefaultUserAgent(),
		"x-perch-locale":     "en",
		"x-perch-call-delta": "1",
		"accept-language":    "*",
		"sec-fetch-mode":     "cors",
	}
}

func applyIdentityHeaders(req *http.Request, delta bool) {
	for key, value := range DefaultHeaders() {
		if key == "x-perch-call-delta" && !delta {
			continue
		}
		req.Header.Set(key, value)
	}
}
