package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	loadscenario "sdmm/internal/aphelion/collab/load"
	"sdmm/internal/aphelion/collab/server"
	"sdmm/internal/aphelion/collab/store/sqlite"
)

const concurrentCommandFixture = `{"seed":260906,"clients":3,"operations":9,"conflict_pairs":2,"max_x":3,"max_y":3,"presence_per_client":3,"target_operations_per_second":80,"target_presence_per_second_per_editor":20}`

func TestLoadRecordedConcurrentScenario(t *testing.T) {
	scenario, err := loadConcurrentScenario(filepath.Join("..", "..", "testdata", "collaboration", "load", "concurrent.json"))
	if err != nil {
		t.Fatal(err)
	}
	if scenario.Config.Clients != 4 || scenario.ExpectedAccepted != 32 || scenario.ExpectedRejected != 8 {
		t.Fatalf("recorded concurrent scenario differs: %#v", scenario.Config)
	}
}

func TestLoadConcurrentScenarioRejectsMalformedManifest(t *testing.T) {
	for name, body := range map[string]string{
		"unknown":    strings.TrimSuffix(concurrentCommandFixture, "}") + `,"unknown":true}`,
		"trailing":   concurrentCommandFixture + `{}`,
		"oversized":  concurrentCommandFixture + strings.Repeat(" ", 1<<20),
		"impossible": strings.Replace(concurrentCommandFixture, `"conflict_pairs":2`, `"conflict_pairs":8`, 1),
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := loadConcurrentScenario(writeConcurrentFixture(t, body)); err == nil {
				t.Fatal("accepted malformed concurrent manifest")
			}
		})
	}
}

func TestConcurrentCommandAppliesRealServiceStream(t *testing.T) {
	testConcurrentCommand(t, false)
}

func TestConcurrentProducedBinary(t *testing.T) {
	if os.Getenv("APHELIONDMM_LOADTEST_BINARY") == "" {
		t.Skip("set APHELIONDMM_LOADTEST_BINARY to exercise the produced command")
	}
	testConcurrentCommand(t, true)
}

func testConcurrentCommand(t *testing.T, binary bool) {
	t.Helper()
	var path string
	if binary {
		path = os.Getenv("APHELIONDMM_LOADTEST_SCENARIO")
	}
	if path == "" {
		path = writeConcurrentFixture(t, concurrentCommandFixture)
	}
	scenario, err := loadConcurrentScenario(path)
	if err != nil {
		t.Fatal(err)
	}
	databasePath := filepath.Join(t.TempDir(), "load.sqlite")
	durable, err := sqlite.Open(databasePath)
	if err != nil {
		t.Fatal(err)
	}
	service := server.NewService(server.ServiceConfig{Store: durable, AllowedOrigins: []string{"http://127.0.0.1"}, PresenceInterval: 16 * time.Millisecond})
	t.Cleanup(func() { _ = service.Shutdown(context.Background()) })
	host := httptest.NewServer(service.Handler())
	t.Cleanup(host.Close)
	token, err := service.NewLaunchTokenForSnapshot(scenario.Initial)
	if err != nil {
		t.Fatal(err)
	}
	body, err := json.Marshal(map[string]any{"snapshot": scenario.Initial})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, host.URL+"/v1/sessions", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Authorization", "Bearer "+token)
	request.Header.Set("Content-Type", "application/json")
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusCreated {
		t.Fatalf("create session status %d", response.StatusCode)
	}
	var session server.CreateSessionResponse
	if err := json.NewDecoder(response.Body).Decode(&session); err != nil {
		t.Fatal(err)
	}
	t.Setenv("APHELIONDMM_LOAD_OWNER_TOKEN", session.OwnerToken)
	t.Setenv(editorTokensFileEnvironment, "")
	args := []string{"-concurrent", "-scenario", path, "-endpoint", host.URL, "-origin", "http://127.0.0.1", "-session", session.SessionID, "-timeout", "10s"}
	var output, diagnostics bytes.Buffer
	if binary {
		command := exec.CommandContext(ctx, os.Getenv("APHELIONDMM_LOADTEST_BINARY"), args...)
		command.Stdout, command.Stderr = &output, &diagnostics
		if err := command.Run(); err != nil {
			t.Fatalf("produced command: %v; diagnostics=%s; result=%s", err, &diagnostics, &output)
		}
	} else if code := run(args, &output, &diagnostics); code != 0 {
		t.Fatalf("command exit %d; diagnostics=%s; result=%s", code, &diagnostics, &output)
	}
	var result loadscenario.ConcurrentResult
	if err := json.Unmarshal(output.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if !result.GatePassed || result.Mode != "concurrent-independent-v1" || result.ScheduledOperations != scenario.Config.Operations || result.StartedOperations != scenario.Config.Operations || result.SentOperations != scenario.Config.Operations || result.AcceptedOperations != scenario.ExpectedAccepted || result.RejectedOperations != scenario.ExpectedRejected || result.AppliedDeliveries != scenario.ExpectedAccepted*scenario.Config.Clients || result.ServerMapHash != scenario.ExpectedMapHash || len(result.Clients) != scenario.Config.Clients {
		t.Fatalf("incorrect command result: %s", &output)
	}
	for _, client := range result.Clients {
		if int(client.Revision) != scenario.ExpectedAccepted || client.MapHash != scenario.ExpectedMapHash || client.AppliedOperations != scenario.ExpectedAccepted {
			t.Fatalf("client differs: %#v", client)
		}
	}
	host.Close()
	if err := service.Shutdown(ctx); err != nil {
		t.Fatal(err)
	}
	reopened, err := sqlite.Open(databasePath)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := reopened.Close(); err != nil {
			t.Error(err)
		}
	}()
	recovery, err := reopened.LoadRecovery(ctx, scenario.Initial.DocumentID)
	if err != nil {
		t.Fatal(err)
	}
	document, err := recovery.Restore()
	if err != nil {
		t.Fatal(err)
	}
	recoveredHash, err := document.Snapshot().Hash()
	if err != nil || int(document.Snapshot().Revision) != scenario.ExpectedAccepted || recoveredHash != scenario.ExpectedMapHash {
		t.Fatalf("durable reopen differs: revision=%d hash=%s err=%v", document.Snapshot().Revision, recoveredHash, err)
	}
	if binary {
		t.Logf("produced command result (SQLite %s; reopen hash matched): %s", reopened.Version(), &output)
	}
}

func TestConcurrentCommandPreservesFailureJSON(t *testing.T) {
	t.Setenv("APHELIONDMM_LOAD_OWNER_TOKEN", "test-credential-must-not-be-logged")
	t.Setenv(editorTokensFileEnvironment, "")
	var output, diagnostics bytes.Buffer
	code := run([]string{"-concurrent", "-scenario", writeConcurrentFixture(t, concurrentCommandFixture), "-endpoint", "invalid", "-timeout", "1s"}, &output, &diagnostics)
	var result loadscenario.ConcurrentResult
	if code != 1 || json.Unmarshal(output.Bytes(), &result) != nil || result.GatePassed || result.Failure == "" || result.PlannedOperations != 9 {
		t.Fatalf("failure lost partial JSON: exit=%d; output=%s; diagnostics=%s", code, &output, &diagnostics)
	}
	if strings.Contains(output.String()+diagnostics.String(), os.Getenv("APHELIONDMM_LOAD_OWNER_TOKEN")) {
		t.Fatal("credential leaked into result")
	}
}

func writeConcurrentFixture(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "concurrent.json")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}
