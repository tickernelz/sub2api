package domain

import "testing"

func TestPerchPlatformIsRegisteredForOpenAIGateway(t *testing.T) {
	spec, ok := LookupPlatform(PlatformPerch)
	if !ok {
		t.Fatalf("perch is not registered")
	}
	if spec.Gateway != PlatformGatewayOpenAI {
		t.Fatalf("gateway = %q, want %q", spec.Gateway, PlatformGatewayOpenAI)
	}
	if !IsConcretePlatform(PlatformPerch) {
		t.Fatalf("perch is not concrete")
	}
	registered := false
	for _, candidate := range ConcretePlatformIDs() {
		if candidate == PlatformPerch {
			registered = true
			break
		}
	}
	if !registered {
		t.Fatalf("perch missing from ConcretePlatformIDs")
	}
}
