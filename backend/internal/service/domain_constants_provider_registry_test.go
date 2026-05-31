package service

import (
	"reflect"
	"testing"

	providerregistry "github.com/tickernelz/sub2api/internal/provider"
)

func TestAllowedQuotaPlatformsFollowProviderRegistry(t *testing.T) {
	want := providerregistry.PlatformQuotaPlatforms()
	if !reflect.DeepEqual(AllowedQuotaPlatforms, want) {
		t.Fatalf("AllowedQuotaPlatforms = %v, want registry quota platforms %v", AllowedQuotaPlatforms, want)
	}
	if IsAllowedQuotaPlatform(PlatformKiro) {
		t.Fatalf("Kiro must remain ineligible for user platform quota")
	}
	if IsAllowedQuotaPlatform(PlatformCursor) {
		t.Fatalf("Cursor quota should stay disabled until the dashboard usage phase")
	}
	if !IsAllowedQuotaPlatform(PlatformOpenCode) {
		t.Fatalf("OpenCode should be eligible for user platform quota")
	}
}
