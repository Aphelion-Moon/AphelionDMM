package main

import (
	"context"
	"testing"

	"sdmm/internal/aphelion/collab/auth"
	"sdmm/internal/aphelion/collab/server"
)

func TestHostedFlowUsesOnlySelectedProviderConfiguration(t *testing.T) {
	provider := server.HostedProviderDiscord
	ttl := "8h"
	config := server.HostedConfig{
		AuthProvider: &provider,
		OIDC: server.HostedOIDC{
			Issuer:       "http://inactive.invalid",
			ClientSecret: server.SecretSource{Environment: "APHELIONDMM_OIDC_CLIENT_SECRET"},
		},
		Discord: server.HostedDiscord{
			ClientID: "123456", GuildID: "112233445566778899",
			RedirectURL:  "https://maps.example.test/v1/auth/complete",
			ClientSecret: server.SecretSource{Environment: "APHELIONDMM_DISCORD_CLIENT_SECRET"},
			SessionTTL:   &ttl,
		},
	}
	var lookups []string
	flow, err := newHostedAuthorizationFlow(context.Background(), config, func(name string) (string, bool) {
		lookups = append(lookups, name)
		return "selected-provider-secret", true
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := flow.(*auth.DiscordFlow); !ok {
		t.Fatalf("flow type = %T, want *auth.DiscordFlow", flow)
	}
	if len(lookups) != 1 || lookups[0] != "APHELIONDMM_DISCORD_CLIENT_SECRET" {
		t.Fatalf("secret lookups = %#v", lookups)
	}
}

func TestHostedFlowDefaultsToOIDCWithoutResolvingDiscordSecret(t *testing.T) {
	config := server.HostedConfig{
		OIDC: server.HostedOIDC{
			Issuer:       "http://invalid.example.test",
			ClientID:     "oidc-client",
			RedirectURL:  "https://maps.example.test/v1/auth/complete",
			ClientSecret: server.SecretSource{Environment: "APHELIONDMM_OIDC_CLIENT_SECRET"},
		},
		Discord: server.HostedDiscord{
			ClientSecret: server.SecretSource{Environment: "APHELIONDMM_DISCORD_CLIENT_SECRET"},
		},
	}
	if config.SelectedAuthProvider() != server.HostedProviderOIDC {
		t.Fatalf("omitted auth provider = %q", config.SelectedAuthProvider())
	}
	var lookups []string
	if _, err := newHostedAuthorizationFlow(context.Background(), config, func(name string) (string, bool) {
		lookups = append(lookups, name)
		return "selected-provider-secret", true
	}); err == nil {
		t.Fatal("invalid OIDC endpoint unexpectedly initialized")
	}
	if len(lookups) != 1 || lookups[0] != "APHELIONDMM_OIDC_CLIENT_SECRET" {
		t.Fatalf("secret lookups = %#v", lookups)
	}
}
