package server

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const validHostedYAML = `
bind_address: "0.0.0.0:8443"
public_origin: "https://maps.example.test"
trusted_proxy_cidrs:
  - "10.0.0.0/8"
database:
  dsn:
    environment: "APHELIONDMM_DATABASE_DSN"
oidc:
  issuer: "https://identity.example.test"
  client_id: "apheliondmm"
  redirect_url: "https://maps.example.test/v1/auth/complete"
  client_secret:
    environment: "APHELIONDMM_OIDC_CLIENT_SECRET"
limits:
  max_connections: 64
  max_operation_changes: 512
  max_websocket_message_bytes: 262144
  max_http_body_bytes: 524288
  max_snapshot_body_bytes: 268435456
telemetry:
  endpoint: "https://telemetry.example.test"
`

func TestLoadHostedConfigAcceptsStrictValidConfiguration(t *testing.T) {
	config, err := LoadHostedConfig(strings.NewReader(validHostedYAML))
	if err != nil {
		t.Fatal(err)
	}
	if config.BindAddress != "0.0.0.0:8443" || config.PublicOrigin != "https://maps.example.test" {
		t.Fatalf("config = %#v", config)
	}
	if config.Limits.MaxConnections != 64 || len(config.TrustedProxyCIDRs) != 1 {
		t.Fatalf("config limits/proxies = %#v", config)
	}
}

func TestHostedBulkResourceBudgetsAreOperatorConfigured(t *testing.T) {
	data := strings.Replace(validHostedYAML, "limits:\n", "limits:\n  bulk_spool_bytes: 1073741824\n  bulk_working_bytes: 4294967296\n", 1)
	config, err := LoadHostedConfig(strings.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	if config.Limits.BulkSpoolBytes != 1<<30 || config.Limits.BulkWorkingBytes != 4<<30 {
		t.Fatal("bulk budget configuration was lost")
	}
	for _, field := range []string{"bulk_spool_bytes", "bulk_working_bytes"} {
		if _, err := LoadHostedConfig(strings.NewReader(strings.Replace(validHostedYAML, "limits:\n", "limits:\n  "+field+": -1\n", 1))); err == nil {
			t.Fatal("negative resource budget accepted")
		}
	}
}

func TestLoadHostedConfigRequiresHTTPSOIDCRedirectURL(t *testing.T) {
	tests := []string{
		strings.Replace(validHostedYAML, "  redirect_url: \"https://maps.example.test/v1/auth/complete\"\n", "", 1),
		strings.Replace(validHostedYAML, "https://maps.example.test/v1/auth/complete", "http://maps.example.test/v1/auth/complete", 1),
	}
	for index, data := range tests {
		if _, err := LoadHostedConfig(strings.NewReader(data)); err == nil {
			t.Errorf("test %d LoadHostedConfig() error = nil", index)
		}
	}
}

func TestSecretSourceResolvesBoundedRegularFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "database.secret")
	if err := os.WriteFile(path, []byte("postgres://file-secret\r\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	secureSecretFixturePermissions(t, path)
	secret, err := (SecretSource{File: path}).Resolve(nil)
	if err != nil {
		t.Fatal(err)
	}
	if secret != "postgres://file-secret" {
		t.Fatalf("secret = %q", secret)
	}
}

func TestSecretSourceRejectsRelativeFile(t *testing.T) {
	if _, err := (SecretSource{File: "database.secret"}).Resolve(nil); err == nil {
		t.Fatal("Resolve() error = nil")
	}
}

func TestLoadHostedConfigRejectsUnknownField(t *testing.T) {
	_, err := LoadHostedConfig(strings.NewReader(validHostedYAML + "unknown_setting: true\n"))
	if err == nil {
		t.Fatal("LoadHostedConfig() error = nil")
	}
}

func TestLoadHostedConfigRejectsInsecureOrAmbiguousNetworkConfiguration(t *testing.T) {
	tests := []string{
		strings.Replace(validHostedYAML, "https://maps.example.test", "http://maps.example.test", 1),
		strings.Replace(validHostedYAML, "10.0.0.0/8", "not-a-network", 1),
		strings.Replace(validHostedYAML, "https://identity.example.test", "http://identity.example.test", 1),
		strings.Replace(validHostedYAML, "https://telemetry.example.test", "http://telemetry.example.test", 1),
		strings.Replace(validHostedYAML, "max_snapshot_body_bytes: 268435456", "max_snapshot_body_bytes: 268435457", 1),
	}
	for index, data := range tests {
		if _, err := LoadHostedConfig(strings.NewReader(data)); err == nil {
			t.Errorf("test %d LoadHostedConfig() error = nil", index)
		}
	}
}

func TestLoadHostedConfigRejectsInlineOrUnscopedSecrets(t *testing.T) {
	tests := []string{
		strings.Replace(validHostedYAML, "environment: \"APHELIONDMM_DATABASE_DSN\"", "environment: \"DATABASE_URL\"", 1),
		strings.Replace(validHostedYAML, "environment: \"APHELIONDMM_DATABASE_DSN\"", "file: \"database.secret\"\n    environment: \"APHELIONDMM_DATABASE_DSN\"", 1),
	}
	for index, data := range tests {
		if _, err := LoadHostedConfig(strings.NewReader(data)); err == nil {
			t.Errorf("test %d LoadHostedConfig() error = nil", index)
		}
	}
}

func TestSecretSourceResolvesNamedEnvironmentWithoutPersistingValue(t *testing.T) {
	source := SecretSource{Environment: "APHELIONDMM_DATABASE_DSN"}
	secret, err := source.Resolve(func(name string) (string, bool) {
		if name != "APHELIONDMM_DATABASE_DSN" {
			t.Fatalf("lookup name = %q", name)
		}
		return "postgres://secret", true
	})
	if err != nil {
		t.Fatal(err)
	}
	if secret != "postgres://secret" || strings.Contains(source.String(), secret) {
		t.Fatalf("secret resolution or redaction failed: source=%s", source.String())
	}
}
