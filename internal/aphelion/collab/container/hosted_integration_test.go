//go:build container

package container_test

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"sdmm/internal/aphelion/collab/testoidc"
)

const postgresImage = "postgres:18.6-bookworm@sha256:1c59e2c3c818eaa0f0628f695b36e7c9e362d6b219b36a54a32df645cbd7e1af"

func TestHostedImageLifecycle(t *testing.T) {
	image := os.Getenv("APHELION_CONTAINER_TEST_IMAGE")
	if image == "" {
		t.Skip("APHELION_CONTAINER_TEST_IMAGE is not set")
	}
	if _, err := exec.LookPath("docker"); err != nil {
		t.Fatalf("locate docker: %v", err)
	}

	fixtureID := fmt.Sprintf("%d-%d", os.Getpid(), time.Now().UnixNano())
	networkName := "aphelion-container-" + fixtureID
	postgresName := "aphelion-postgres-" + fixtureID
	hostedName := "aphelion-hosted-" + fixtureID
	docker(t, "network", "create", networkName)
	t.Cleanup(func() {
		dockerCleanup(hostedName, postgresName, networkName)
	})

	root := t.TempDir()
	clientSecret := "container-fixture-client-credential"
	appPort := availablePort(t)
	oidcPort, caPEM, oidcFixture, telemetrySink, stopOIDC := startOIDCFixture(t, clientSecret)
	t.Cleanup(stopOIDC)
	caPath := filepath.Join(root, "fixture-ca.pem")
	// These bind mounts contain public configuration and a public CA only;
	// credentials are passed separately. The image's non-root UID must read them.
	if err := os.WriteFile(caPath, caPEM, 0o644); err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(root, "hosted.yaml")
	config := fmt.Sprintf(`bind_address: "0.0.0.0:8080"
public_origin: "https://maps.example.test"
trusted_proxy_cidrs: []
database:
  dsn:
    environment: "APHELIONDMM_DATABASE_DSN"
oidc:
  issuer: "https://host.docker.internal:%d"
  client_id: "apheliondmm-container-fixture"
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
  endpoint: "https://host.docker.internal:%d"
`, oidcPort, oidcPort)
	if err := os.WriteFile(configPath, []byte(config), 0o644); err != nil {
		t.Fatal(err)
	}

	docker(t, "run", "--detach", "--name", postgresName, "--network", networkName,
		"--env", "POSTGRES_USER=aphelion_fixture", "--env", "POSTGRES_PASSWORD=fixture-db-only", "--env", "POSTGRES_DB=aphelion_fixture",
		"--health-cmd", "pg_isready -h 127.0.0.1 -U aphelion_fixture -d aphelion_fixture", "--health-interval", "1s", "--health-timeout", "2s", "--health-retries", "30", postgresImage)
	waitDockerHealth(t, postgresName, "healthy", time.Minute)

	databaseDSN := fmt.Sprintf("postgres://aphelion_fixture:fixture-db-only@%s:5432/aphelion_fixture?sslmode=disable", postgresName)
	docker(t, "run", "--detach", "--name", hostedName, "--network", networkName, "--add-host", "host.docker.internal:host-gateway",
		"--publish", fmt.Sprintf("127.0.0.1:%d:8080", appPort), "--read-only", "--tmpfs", "/tmp:rw,noexec,nosuid,size=16m",
		"--cap-drop", "ALL", "--security-opt", "no-new-privileges:true",
		"--mount", fmt.Sprintf("type=bind,source=%s,target=/etc/apheliondmm/config.yaml,readonly", configPath),
		"--mount", fmt.Sprintf("type=bind,source=%s,target=/etc/apheliondmm/fixture-ca.pem,readonly", caPath),
		"--env", "SSL_CERT_FILE=/etc/apheliondmm/fixture-ca.pem", "--env", "APHELIONDMM_DATABASE_DSN="+databaseDSN,
		"--env", "APHELIONDMM_OIDC_CLIENT_SECRET="+clientSecret, image)

	baseURL := fmt.Sprintf("http://127.0.0.1:%d", appPort)
	waitHostedStatus(t, baseURL+"/v1/health/ready", http.StatusOK, hostedName, time.Minute)
	assertHardenedRuntime(t, hostedName)

	client := &http.Client{Timeout: 10 * time.Second}
	token := authenticate(t, client, baseURL, oidcPort, caPEM)
	sessionID := createHostedSession(t, client, baseURL, token)
	invitationToken := createInvitation(t, client, baseURL, sessionID, token)
	if shape := strings.TrimSpace(docker(t, "exec", postgresName, "psql", "-U", "aphelion_fixture", "-d", "aphelion_fixture", "-Atc", "SELECT count(*) || ':' || min(octet_length(token_hash)) || ':' || max(octet_length(token_hash)) FROM collaboration_hosted_invitations;")); shape != "1:32:32" {
		t.Fatalf("invitation database shape = %q", shape)
	}
	getSnapshot(t, client, baseURL, sessionID, token, http.StatusOK)
	if err := oidcFixture.SetIdentity("container-fixture-editor", "Container Editor"); err != nil {
		t.Fatal(err)
	}
	editorToken := authenticate(t, client, baseURL, oidcPort, caPEM)
	redeemBody, err := json.Marshal(map[string]string{"token": invitationToken})
	if err != nil {
		t.Fatal(err)
	}
	redeemed := request(t, client, http.MethodPost, baseURL+"/v1/sessions/"+sessionID+"/hosted-invitations/redeem", editorToken, redeemBody, http.StatusOK)
	_ = redeemed.Body.Close()
	reused := request(t, client, http.MethodPost, baseURL+"/v1/sessions/"+sessionID+"/hosted-invitations/redeem", editorToken, redeemBody, http.StatusUnauthorized)
	_ = reused.Body.Close()
	getSnapshot(t, client, baseURL, sessionID, editorToken, http.StatusOK)
	if err := oidcFixture.SetIdentity("container-fixture-subject", "Container Fixture"); err != nil {
		t.Fatal(err)
	}

	docker(t, "stop", "--timeout", "10", hostedName)
	if exitCode := strings.TrimSpace(docker(t, "inspect", "--format", "{{.State.ExitCode}}", hostedName)); exitCode != "0" {
		t.Fatalf("hosted graceful exit code = %s", exitCode)
	}
	if telemetrySink.traces.Load() == 0 || telemetrySink.metrics.Load() == 0 {
		t.Fatalf("OTLP exports after graceful shutdown = traces:%d metrics:%d", telemetrySink.traces.Load(), telemetrySink.metrics.Load())
	}
	docker(t, "start", hostedName)
	waitHTTPStatus(t, baseURL+"/v1/health/ready", http.StatusOK, time.Minute)
	token = authenticate(t, client, baseURL, oidcPort, caPEM)
	getSnapshot(t, client, baseURL, sessionID, token, http.StatusOK)

	docker(t, "stop", "--timeout", "10", postgresName)
	waitHTTPStatus(t, baseURL+"/v1/health/ready", http.StatusServiceUnavailable, 30*time.Second)
	if state := strings.TrimSpace(docker(t, "inspect", "--format", "{{.State.Status}}", hostedName)); state != "running" {
		t.Fatalf("hosted state during database loss = %s", state)
	}
	docker(t, "start", postgresName)
	waitDockerHealth(t, postgresName, "healthy", time.Minute)
	waitHTTPStatus(t, baseURL+"/v1/health/ready", http.StatusOK, time.Minute)

	request(t, client, http.MethodPost, baseURL+"/v1/auth/logout", token, nil, http.StatusNoContent)
	getSnapshot(t, client, baseURL, sessionID, token, http.StatusUnauthorized)
}

type otlpSink struct {
	traces  atomic.Int64
	metrics atomic.Int64
}

func startOIDCFixture(t *testing.T, clientSecret string) (int, []byte, *testoidc.Fixture, *otlpSink, func()) {
	t.Helper()
	listener, err := net.Listen("tcp", "0.0.0.0:0")
	if err != nil {
		t.Fatal(err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	issuer := fmt.Sprintf("https://host.docker.internal:%d", port)
	fixture, err := testoidc.New(testoidc.Config{
		Issuer: issuer, ClientID: "apheliondmm-container-fixture", ClientSecret: clientSecret,
		RedirectURL: "https://maps.example.test/v1/auth/complete", Subject: "container-fixture-subject", DisplayName: "Container Fixture",
	})
	if err != nil {
		_ = listener.Close()
		t.Fatal(err)
	}
	certificate, caPEM, err := testoidc.GenerateLocalTLSCertificate([]string{"host.docker.internal", "localhost", "127.0.0.1"}, time.Now())
	if err != nil {
		_ = listener.Close()
		t.Fatal(err)
	}
	sink := &otlpSink{}
	handler := http.NewServeMux()
	handler.Handle("/", fixture.Handler())
	handler.HandleFunc("POST /v1/traces", func(writer http.ResponseWriter, request *http.Request) {
		_, _ = io.Copy(io.Discard, http.MaxBytesReader(writer, request.Body, 1<<20))
		sink.traces.Add(1)
		writer.Header().Set("Content-Type", "application/x-protobuf")
		writer.WriteHeader(http.StatusOK)
	})
	handler.HandleFunc("POST /v1/metrics", func(writer http.ResponseWriter, request *http.Request) {
		_, _ = io.Copy(io.Discard, http.MaxBytesReader(writer, request.Body, 1<<20))
		sink.metrics.Add(1)
		writer.Header().Set("Content-Type", "application/x-protobuf")
		writer.WriteHeader(http.StatusOK)
	})
	server := &http.Server{Handler: handler, ReadHeaderTimeout: 5 * time.Second}
	tlsListener := tls.NewListener(listener, &tls.Config{Certificates: []tls.Certificate{certificate}, MinVersion: tls.VersionTLS12})
	go func() { _ = server.Serve(tlsListener) }()
	return port, caPEM, fixture, sink, func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = server.Shutdown(ctx)
	}
}

func authenticate(t *testing.T, client *http.Client, baseURL string, oidcPort int, caPEM []byte) string {
	t.Helper()
	begin := request(t, client, http.MethodPost, baseURL+"/v1/auth/begin", "", nil, http.StatusOK)
	var beginBody struct {
		AuthorizationURL string `json:"authorization_url"`
	}
	decodeJSON(t, begin, &beginBody)
	authorizeURL, err := url.Parse(beginBody.AuthorizationURL)
	if err != nil {
		t.Fatal(err)
	}
	authorizeURL.Host = net.JoinHostPort("localhost", strconv.Itoa(oidcPort))
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(caPEM) {
		t.Fatal("append fixture CA")
	}
	authorizeClient := &http.Client{
		Timeout:       10 * time.Second,
		Transport:     &http.Transport{TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS12, RootCAs: roots}},
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
	authorize, err := authorizeClient.Get(authorizeURL.String())
	if err != nil {
		t.Fatal(err)
	}
	_ = authorize.Body.Close()
	if authorize.StatusCode != http.StatusFound {
		t.Fatalf("authorize status = %d", authorize.StatusCode)
	}
	callback, err := url.Parse(authorize.Header.Get("Location"))
	if err != nil {
		t.Fatal(err)
	}
	complete := request(t, client, http.MethodGet, baseURL+"/v1/auth/complete?"+callback.RawQuery, "", nil, http.StatusOK)
	var result struct {
		Token string `json:"token"`
	}
	decodeJSON(t, complete, &result)
	if result.Token == "" {
		t.Fatal("hosted authentication token is empty")
	}
	return result.Token
}

func createHostedSession(t *testing.T, client *http.Client, baseURL, token string) string {
	t.Helper()
	body := []byte(`{"snapshot":{"protocol_version":1,"schema_version":1,"document_id":"01890f3e-7b5c-7abc-8def-0123456789ab","revision":0,"environment_hash":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","max_x":3,"max_y":1,"max_z":1,"tiles":[]}}`)
	response := request(t, client, http.MethodPost, baseURL+"/v1/hosted/sessions", token, body, http.StatusCreated)
	var created struct {
		SessionID string `json:"session_id"`
	}
	decodeJSON(t, response, &created)
	return created.SessionID
}

func createInvitation(t *testing.T, client *http.Client, baseURL, sessionID, token string) string {
	t.Helper()
	response := request(t, client, http.MethodPost, baseURL+"/v1/sessions/"+sessionID+"/hosted-invitations", token, []byte(`{"role":"editor"}`), http.StatusCreated)
	if response.Header.Get("Cache-Control") != "no-store" {
		t.Fatalf("invitation Cache-Control = %q", response.Header.Get("Cache-Control"))
	}
	var invitation struct {
		Token string `json:"token"`
	}
	decodeJSON(t, response, &invitation)
	if invitation.Token == "" {
		t.Fatal("hosted invitation token is empty")
	}
	return invitation.Token
}

func getSnapshot(t *testing.T, client *http.Client, baseURL, sessionID, token string, status int) {
	t.Helper()
	response := request(t, client, http.MethodGet, baseURL+"/v1/sessions/"+sessionID+"/snapshot", token, nil, status)
	_ = response.Body.Close()
}

func request(t *testing.T, client *http.Client, method, target, token string, body []byte, expected int) *http.Response {
	t.Helper()
	request, err := http.NewRequestWithContext(context.Background(), method, target, bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	if token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
	}
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	response, err := client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != expected {
		data, _ := io.ReadAll(io.LimitReader(response.Body, 4096))
		_ = response.Body.Close()
		t.Fatalf("%s %s status = %d, want %d: %s", method, target, response.StatusCode, expected, data)
	}
	return response
}

func decodeJSON(t *testing.T, response *http.Response, target any) {
	t.Helper()
	defer func() { _ = response.Body.Close() }()
	if err := json.NewDecoder(io.LimitReader(response.Body, 1<<20)).Decode(target); err != nil {
		t.Fatal(err)
	}
}

func assertHardenedRuntime(t *testing.T, name string) {
	t.Helper()
	inspection := strings.TrimSpace(docker(t, "inspect", "--format", "{{.HostConfig.ReadonlyRootfs}}|{{.Config.User}}|{{json .HostConfig.CapDrop}}|{{json .HostConfig.SecurityOpt}}", name))
	if inspection != `true|nonroot:nonroot|["ALL"]|["no-new-privileges:true"]` {
		t.Fatalf("hosted runtime hardening = %s", inspection)
	}
}

func availablePort(t *testing.T) int {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	_ = listener.Close()
	return port
}

func waitHTTPStatus(t *testing.T, target string, expected int, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		response, err := (&http.Client{Timeout: 3 * time.Second}).Get(target)
		if err == nil {
			_ = response.Body.Close()
			if response.StatusCode == expected {
				return
			}
		}
		time.Sleep(250 * time.Millisecond)
	}
	t.Fatalf("%s did not reach HTTP status %d", target, expected)
}

func waitHostedStatus(t *testing.T, target string, expected int, hostedName string, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		response, err := (&http.Client{Timeout: 3 * time.Second}).Get(target)
		if err == nil {
			_ = response.Body.Close()
			if response.StatusCode == expected {
				return
			}
		}
		stateCommand := exec.Command("docker", "inspect", "--format", "{{.State.Status}}", hostedName)
		if state, stateErr := stateCommand.Output(); stateErr == nil && strings.TrimSpace(string(state)) == "exited" {
			break
		}
		time.Sleep(250 * time.Millisecond)
	}
	logsCommand := exec.Command("docker", "logs", hostedName)
	logs, _ := logsCommand.CombinedOutput()
	t.Fatalf("%s did not reach HTTP status %d; hosted logs:\n%s", target, expected, logs)
}

func waitDockerHealth(t *testing.T, name, expected string, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		command := exec.Command("docker", "inspect", "--format", "{{.State.Health.Status}}", name)
		output, err := command.Output()
		if err == nil && strings.TrimSpace(string(output)) == expected {
			return
		}
		time.Sleep(250 * time.Millisecond)
	}
	t.Fatalf("container %s did not reach health %s", name, expected)
}

func docker(t *testing.T, arguments ...string) string {
	t.Helper()
	command := exec.Command("docker", arguments...)
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("docker %s: %v\n%s", strings.Join(arguments, " "), err, output)
	}
	return string(output)
}

func dockerCleanup(hostedName, postgresName, networkName string) {
	for _, name := range []string{hostedName, postgresName} {
		command := exec.Command("docker", "rm", "--force", name)
		_, _ = command.CombinedOutput()
	}
	command := exec.Command("docker", "network", "rm", networkName)
	_, _ = command.CombinedOutput()
}
