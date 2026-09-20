package ui

import "testing"

func TestDefaultHostedOriginIsPublicAphelionService(t *testing.T) {
	if DefaultHostedOrigin != "https://mapcollab.a13.info" {
		t.Fatalf("DefaultHostedOrigin = %q", DefaultHostedOrigin)
	}
	if err := validateHostedEndpoint(DefaultHostedOrigin); err != nil {
		t.Fatalf("default hosted origin is invalid: %v", err)
	}
}
