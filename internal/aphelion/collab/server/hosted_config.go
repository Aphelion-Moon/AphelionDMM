package server

import (
	"errors"
	"fmt"
	"io"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"

	"sdmm/internal/aphelion/collab/protocol"
)

const maxHostedConfigBytes = 1 << 20

var secretEnvironmentPattern = regexp.MustCompile(`^APHELIONDMM_[A-Z0-9_]+$`)

type SecretSource struct {
	Environment string `yaml:"environment,omitempty" json:"environment,omitempty"`
	File        string `yaml:"file,omitempty" json:"file,omitempty"`
}

type HostedConfig struct {
	BindAddress       string          `yaml:"bind_address"`
	PublicOrigin      string          `yaml:"public_origin"`
	TrustedProxyCIDRs []string        `yaml:"trusted_proxy_cidrs"`
	Database          HostedDatabase  `yaml:"database"`
	OIDC              HostedOIDC      `yaml:"oidc"`
	Limits            HostedLimits    `yaml:"limits"`
	Telemetry         HostedTelemetry `yaml:"telemetry"`
	trustedProxies    []*net.IPNet
}

type HostedDatabase struct {
	DSN SecretSource `yaml:"dsn"`
}

type HostedOIDC struct {
	Issuer       string       `yaml:"issuer"`
	ClientID     string       `yaml:"client_id"`
	RedirectURL  string       `yaml:"redirect_url"`
	ClientSecret SecretSource `yaml:"client_secret"`
}

type HostedLimits struct {
	MaxConnections           int   `yaml:"max_connections"`
	MaxOperationChanges      int   `yaml:"max_operation_changes"`
	MaxWebSocketMessageBytes int64 `yaml:"max_websocket_message_bytes"`
	MaxHTTPBodyBytes         int64 `yaml:"max_http_body_bytes"`
	MaxSnapshotBodyBytes     int64 `yaml:"max_snapshot_body_bytes"`
}

type HostedTelemetry struct {
	Endpoint string `yaml:"endpoint"`
}

func LoadHostedConfig(reader io.Reader) (HostedConfig, error) {
	decoder := yaml.NewDecoder(io.LimitReader(reader, maxHostedConfigBytes+1))
	decoder.KnownFields(true)
	var config HostedConfig
	if err := decoder.Decode(&config); err != nil {
		return HostedConfig{}, fmt.Errorf("decode hosted configuration: %w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		if err == nil {
			return HostedConfig{}, fmt.Errorf("decode hosted configuration: multiple YAML documents are forbidden")
		}
		return HostedConfig{}, fmt.Errorf("decode hosted configuration trailer: %w", err)
	}
	if err := config.validate(); err != nil {
		return HostedConfig{}, err
	}
	return config, nil
}

func (config *HostedConfig) validate() error {
	if strings.TrimSpace(config.BindAddress) == "" {
		return fmt.Errorf("hosted bind address is required")
	}
	if err := ValidateListenAddress(config.BindAddress, true); err != nil {
		return fmt.Errorf("validate hosted bind address: %w", err)
	}
	if err := validateHTTPSOrigin("public origin", config.PublicOrigin); err != nil {
		return err
	}
	config.trustedProxies = make([]*net.IPNet, 0, len(config.TrustedProxyCIDRs))
	for _, value := range config.TrustedProxyCIDRs {
		_, network, err := net.ParseCIDR(value)
		if err != nil {
			return fmt.Errorf("parse trusted proxy CIDR %q: %w", value, err)
		}
		config.trustedProxies = append(config.trustedProxies, network)
	}
	if err := config.Database.DSN.validate("database DSN"); err != nil {
		return err
	}
	if err := validateOIDCIssuer(config.OIDC.Issuer); err != nil {
		return err
	}
	if strings.TrimSpace(config.OIDC.ClientID) == "" || len(config.OIDC.ClientID) > protocol.MaxIdentifierBytes {
		return fmt.Errorf("OIDC client ID is required and must be at most %d bytes", protocol.MaxIdentifierBytes)
	}
	if err := validateOIDCRedirect(config.PublicOrigin, config.OIDC.RedirectURL); err != nil {
		return err
	}
	if err := config.OIDC.ClientSecret.validate("OIDC client secret"); err != nil {
		return err
	}
	if config.Telemetry.Endpoint != "" {
		if err := validateHTTPSOrigin("telemetry endpoint", config.Telemetry.Endpoint); err != nil {
			return err
		}
	}
	if config.Limits.MaxConnections <= 0 || config.Limits.MaxOperationChanges <= 0 || config.Limits.MaxOperationChanges > protocol.MaxOperationChanges {
		return fmt.Errorf("hosted connection and operation limits are invalid")
	}
	if config.Limits.MaxWebSocketMessageBytes <= 0 || config.Limits.MaxWebSocketMessageBytes > protocol.MaxMessageBytes {
		return fmt.Errorf("hosted WebSocket message limit is invalid")
	}
	if config.Limits.MaxHTTPBodyBytes <= 0 || config.Limits.MaxHTTPBodyBytes > MaxHTTPBodyBytes {
		return fmt.Errorf("hosted HTTP body limit is invalid")
	}
	if config.Limits.MaxSnapshotBodyBytes <= 0 || config.Limits.MaxSnapshotBodyBytes > MaxSnapshotBodyBytes {
		return fmt.Errorf("hosted snapshot body limit is invalid")
	}
	return nil
}

func (source SecretSource) validate(name string) error {
	if (source.Environment == "") == (source.File == "") {
		return fmt.Errorf("%s must specify exactly one environment or file source", name)
	}
	if source.Environment != "" && !secretEnvironmentPattern.MatchString(source.Environment) {
		return fmt.Errorf("%s environment %q is outside the APHELIONDMM namespace", name, source.Environment)
	}
	if source.File != "" && !filepath.IsAbs(source.File) {
		return fmt.Errorf("%s file path must be absolute", name)
	}
	return nil
}

func (source SecretSource) Resolve(lookup func(string) (string, bool)) (string, error) {
	if err := source.validate("secret"); err != nil {
		return "", err
	}
	if source.Environment != "" {
		value, found := lookup(source.Environment)
		if !found || value == "" {
			return "", fmt.Errorf("secret environment %q is unavailable", source.Environment)
		}
		return value, nil
	}
	info, err := os.Lstat(source.File)
	if err != nil {
		return "", fmt.Errorf("inspect secret file: %w", err)
	}
	if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
		return "", fmt.Errorf("secret file must be a regular non-symlink file")
	}
	permissionsAllowed, err := secretFilePermissionsAllowed(source.File, info.Mode())
	if err != nil {
		return "", fmt.Errorf("inspect secret file permissions %s: %w", source.File, err)
	}
	if !permissionsAllowed {
		return "", fmt.Errorf("secret file %s permissions grant untrusted read access", source.File)
	}
	file, err := os.Open(source.File)
	if err != nil {
		return "", fmt.Errorf("open secret file: %w", err)
	}
	defer func() { _ = file.Close() }()
	data, err := io.ReadAll(io.LimitReader(file, (64<<10)+1))
	if err != nil {
		return "", fmt.Errorf("read secret file: %w", err)
	}
	if len(data) > 64<<10 {
		return "", fmt.Errorf("secret file exceeds 64 KiB")
	}
	value := strings.TrimRight(string(data), "\r\n")
	if value == "" || strings.ContainsRune(value, 0) {
		return "", fmt.Errorf("secret file is empty or invalid")
	}
	return value, nil
}

func (source SecretSource) String() string {
	if source.Environment != "" {
		return "environment:" + source.Environment
	}
	if source.File != "" {
		return "file:" + source.File
	}
	return "unconfigured"
}

func validateHTTPSOrigin(name, value string) error {
	parsed, err := url.Parse(value)
	if err != nil {
		return fmt.Errorf("parse %s: %w", name, err)
	}
	if parsed.Scheme != "https" || parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" || (parsed.Path != "" && parsed.Path != "/") {
		return fmt.Errorf("%s must be an HTTPS origin without credentials, path, query, or fragment", name)
	}
	return nil
}

// OIDC providers may scope issuers by path, as Cloudflare Access does for
// individual SaaS clients. This is distinct from the browser's public origin.
func validateOIDCIssuer(value string) error {
	parsed, err := url.Parse(value)
	if err != nil || parsed.Scheme != "https" || parsed.Hostname() == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.ForceQuery || parsed.Fragment != "" || strings.Contains(value, "#") {
		return fmt.Errorf("OIDC issuer must be an HTTPS URL without credentials, query, or fragment")
	}
	return nil
}

func validateOIDCRedirect(publicOrigin, value string) error {
	redirect, err := url.Parse(value)
	if err != nil || redirect.Scheme != "https" || redirect.Host == "" || redirect.User != nil || redirect.RawQuery != "" || redirect.Fragment != "" {
		return fmt.Errorf("OIDC redirect URL must be an HTTPS URL without credentials, query, or fragment")
	}
	public, err := url.Parse(publicOrigin)
	if err != nil || !strings.EqualFold(redirect.Scheme, public.Scheme) || !strings.EqualFold(redirect.Host, public.Host) || redirect.Path != "/v1/auth/complete" {
		return fmt.Errorf("OIDC redirect URL must be the public origin /v1/auth/complete endpoint")
	}
	return nil
}
