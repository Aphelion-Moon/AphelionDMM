package protocol

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"sdmm/internal/aphelion/collab/model"

	"gopkg.in/yaml.v3"
)

func TestClientFixturesDecode(t *testing.T) {
	t.Parallel()

	for _, name := range []string{
		"client_join.json",
		"client_operation_submit.json",
		"client_inverse_request.json",
		"client_presence_update.json",
		"client_acknowledged_revision.json",
		"client_ping.json",
	} {
		name := name
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			data := readFixture(t, name)
			if _, err := DecodeClient(data); err != nil {
				t.Fatalf("DecodeClient() error = %v", err)
			}
		})
	}
}

func TestServerFixturesDecode(t *testing.T) {
	t.Parallel()

	for _, name := range []string{
		"server_joined.json",
		"server_operation_accepted.json",
		"server_operation_rejected.json",
		"server_replay_complete.json",
		"server_presence_snapshot.json",
		"server_presence_update.json",
		"server_session_notice.json",
		"server_pong.json",
	} {
		name := name
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			data := readFixture(t, name)
			if _, err := DecodeServer(data); err != nil {
				t.Fatalf("DecodeServer() error = %v", err)
			}
		})
	}
}

func TestDecodeClientRejectsInvalidEnvelope(t *testing.T) {
	t.Parallel()

	valid := readFixture(t, "client_ping.json")
	tests := map[string][]byte{
		"unknown envelope field": []byte(`{"protocol_version":1,"message_id":"m","session_id":"s","type":"ping","payload":{"nonce":"n"},"extra":true}`),
		"unknown message type":   []byte(`{"protocol_version":1,"message_id":"m","session_id":"s","type":"execute","payload":{}}`),
		"missing version":        []byte(`{"message_id":"m","session_id":"s","type":"ping","payload":{"nonce":"n"}}`),
		"oversized identifier":   []byte(strings.Replace(string(valid), `"message_id":"message-1"`, `"message_id":"`+strings.Repeat("x", MaxIdentifierBytes+1)+`"`, 1)),
		"trailing document":      append(append([]byte(nil), valid...), []byte(` {}`)...),
	}
	for name, data := range tests {
		name, data := name, data
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if _, err := DecodeClient(data); err == nil {
				t.Fatal("DecodeClient() error = nil")
			}
		})
	}
}

func TestDecodeClientRejectsInvalidOperation(t *testing.T) {
	t.Parallel()

	data := readFixture(t, "client_operation_submit.json")
	var envelope map[string]any
	if err := json.Unmarshal(data, &envelope); err != nil {
		t.Fatal(err)
	}
	payload := envelope["payload"].(map[string]any)
	operation := payload["operation"].(map[string]any)
	operation["environment_hash"] = "not-a-sha256"
	data, err := json.Marshal(envelope)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := DecodeClient(data); err == nil {
		t.Fatal("DecodeClient() error = nil")
	}
}

func FuzzDecodeClient(f *testing.F) {
	for _, seed := range [][]byte{
		{},
		[]byte(`{}`),
		[]byte(`{"protocol_version":1,"message_id":"m","session_id":"s","type":"ping","payload":{"nonce":"n"}}`),
	} {
		f.Add(seed)
	}
	f.Fuzz(func(_ *testing.T, data []byte) {
		_, _ = DecodeClient(data)
	})
}

func FuzzDecodeServer(f *testing.F) {
	for _, seed := range [][]byte{
		{},
		[]byte(`{}`),
		[]byte(`{"protocol_version":1,"message_id":"m","session_id":"s","type":"pong","payload":{"nonce":"n"}}`),
	} {
		f.Add(seed)
	}
	f.Fuzz(func(_ *testing.T, data []byte) {
		_, _ = DecodeServer(data)
	})
}

func TestDecodeServerRejectsMissingPresenceInterval(t *testing.T) {
	t.Parallel()

	data := readFixture(t, "server_joined.json")
	var envelope map[string]any
	if err := json.Unmarshal(data, &envelope); err != nil {
		t.Fatal(err)
	}
	delete(envelope["payload"].(map[string]any), "presence_interval_ms")
	data, err := json.Marshal(envelope)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := DecodeServer(data); err == nil {
		t.Fatal("DecodeServer() accepted a joined message without a presence interval")
	}
}

func TestDecodeServerRejectsMissingResumptionCredential(t *testing.T) {
	t.Parallel()

	data := readFixture(t, "server_joined.json")
	var envelope map[string]any
	if err := json.Unmarshal(data, &envelope); err != nil {
		t.Fatal(err)
	}
	payload := envelope["payload"].(map[string]any)
	delete(payload, "resumption_token")
	delete(payload, "resumption_token_expires_at")
	data, err := json.Marshal(envelope)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := DecodeServer(data); err == nil {
		t.Fatal("DecodeServer() accepted a joined message without a resumption credential")
	}
}

func TestPresenceSelectionIsBoundedAndNormalized(t *testing.T) {
	t.Parallel()

	selection := &PresenceSelection{Min: model.Coord{X: 2, Y: 3, Z: 1}, Max: model.Coord{X: 4, Y: 5, Z: 1}}
	payload := PresenceUpdatePayload{Sequence: 1, Cursor: &model.Coord{X: 2, Y: 3, Z: 1}, Selection: selection, Status: "active"}
	encoded, err := json.Marshal(ClientEnvelope{ProtocolVersion: model.ProtocolVersion, MessageID: "presence", SessionID: "session", Type: ClientPresenceUpdate, Payload: mustMarshalPayload(t, payload)})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := DecodeClient(encoded); err != nil {
		t.Fatalf("DecodeClient() rejected normalized selection: %v", err)
	}

	selection.Min.X, selection.Max.X = 65, 1
	encoded, err = json.Marshal(ClientEnvelope{ProtocolVersion: model.ProtocolVersion, MessageID: "presence", SessionID: "session", Type: ClientPresenceUpdate, Payload: mustMarshalPayload(t, payload)})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := DecodeClient(encoded); err == nil {
		t.Fatal("DecodeClient() accepted reversed selection bounds")
	}

	selection.Min = model.Coord{X: 1, Y: 1, Z: 1}
	selection.Max = model.Coord{X: 65, Y: 65, Z: 1}
	encoded, err = json.Marshal(ClientEnvelope{ProtocolVersion: model.ProtocolVersion, MessageID: "presence", SessionID: "session", Type: ClientPresenceUpdate, Payload: mustMarshalPayload(t, payload)})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := DecodeClient(encoded); err == nil {
		t.Fatal("DecodeClient() accepted a selection larger than 4096 tiles")
	}
}

func mustMarshalPayload(t *testing.T, payload any) json.RawMessage {
	t.Helper()
	encoded, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	return encoded
}

func TestDecodeClientValidatesProfileUpdate(t *testing.T) {
	t.Parallel()
	encode := func(displayName string) []byte {
		data, err := json.Marshal(ClientEnvelope{ProtocolVersion: model.ProtocolVersion, MessageID: "profile", SessionID: "session", Type: ClientProfileUpdate, Payload: mustMarshalPayload(t, ProfileUpdatePayload{DisplayName: displayName})})
		if err != nil {
			t.Fatal(err)
		}
		return data
	}
	decoded, err := DecodeClient(encode("Test Owner"))
	if err != nil {
		t.Fatal(err)
	}
	if payload := decoded.Payload.(*ProfileUpdatePayload); payload.DisplayName != "Test Owner" {
		t.Fatalf("display name = %q, want Test Owner", payload.DisplayName)
	}
	for _, invalid := range []string{"", "   ", strings.Repeat("x", MaxDisplayNameBytes+1)} {
		if _, err := DecodeClient(encode(invalid)); err == nil {
			t.Fatalf("DecodeClient() accepted invalid display name %q", invalid)
		}
	}
}

// APHELION EDIT ADDITION START - COLLABORATION CURSOR COLOR
func TestProfileUpdateCursorColorIsOptionalAndAdditive(t *testing.T) {
	t.Parallel()
	encode := func(payload string) []byte {
		data, err := json.Marshal(ClientEnvelope{ProtocolVersion: model.ProtocolVersion, MessageID: "profile", SessionID: "session", Type: ClientProfileUpdate, Payload: json.RawMessage(payload)})
		if err != nil {
			t.Fatal(err)
		}
		return data
	}
	// Absence keeps the legacy shape and meaning.
	legacy, err := DecodeClient(encode(`{"display_name":"Test Owner"}`))
	if err != nil {
		t.Fatal(err)
	}
	if got := legacy.Payload.(*ProfileUpdatePayload); got.CursorColor != nil || got.DisplayName != "Test Owner" {
		t.Fatalf("legacy payload = %+v", got)
	}
	// Name and colour together, colour zero is a real value.
	both, err := DecodeClient(encode(`{"display_name":"Test Owner","cursor_color":0}`))
	if err != nil {
		t.Fatal(err)
	}
	if got := both.Payload.(*ProfileUpdatePayload); got.CursorColor == nil || *got.CursorColor != 0 {
		t.Fatalf("cursor color = %v, want 0", got.CursorColor)
	}
	// Colour-only update.
	only, err := DecodeClient(encode(`{"cursor_color":5}`))
	if err != nil {
		t.Fatal(err)
	}
	if got := only.Payload.(*ProfileUpdatePayload); got.DisplayName != "" || got.CursorColor == nil || *got.CursorColor != 5 {
		t.Fatalf("colour-only payload = %+v", got)
	}
	// Out-of-range indices decode so the server can answer with a bounded notice
	// instead of closing the socket; range is enforced by ValidCursorColor.
	if _, err := DecodeClient(encode(`{"cursor_color":999}`)); err != nil {
		t.Fatalf("DecodeClient() rejected out-of-range colour at the codec layer: %v", err)
	}
	for _, invalid := range []string{`{}`, `{"display_name":"   "}`, `{"cursor_color":"red"}`} {
		if _, err := DecodeClient(encode(invalid)); err == nil {
			t.Fatalf("DecodeClient() accepted invalid profile update %s", invalid)
		}
	}
	// Omitted when nil so legacy servers see the unchanged shape.
	legacyJSON := string(mustMarshalPayload(t, ProfileUpdatePayload{DisplayName: "Name"}))
	if strings.Contains(legacyJSON, "cursor_color") {
		t.Fatalf("legacy encoding leaked cursor_color: %s", legacyJSON)
	}
}

func TestValidCursorColor(t *testing.T) {
	t.Parallel()
	for index := 0; index < CursorColorPaletteSize; index++ {
		if !ValidCursorColor(index) {
			t.Fatalf("ValidCursorColor(%d) = false", index)
		}
	}
	for _, index := range []int{-1, CursorColorPaletteSize, 1 << 30} {
		if ValidCursorColor(index) {
			t.Fatalf("ValidCursorColor(%d) = true", index)
		}
	}
}

func TestServerPresenceCursorColorFixtureAndAbsence(t *testing.T) {
	t.Parallel()
	for name, want := range map[string]*int{"server_presence_update.json": nil, "server_presence_update_cursor_color.json": intPointer(3)} {
		decoded, err := DecodeServer(readFixture(t, name))
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		got := decoded.Payload.(*ServerPresenceUpdatePayload).CursorColor
		if (got == nil) != (want == nil) || (got != nil && *got != *want) {
			t.Fatalf("%s cursor color = %v, want %v", name, got, want)
		}
	}
	// A newer server may use a larger palette; the codec stays lenient and
	// desktop rendering falls back to the hash default.
	data := strings.Replace(string(readFixture(t, "server_presence_update_cursor_color.json")), `"cursor_color":3`, `"cursor_color":99`, 1)
	if _, err := DecodeServer([]byte(data)); err != nil {
		t.Fatalf("DecodeServer() rejected a larger palette index: %v", err)
	}
	if _, err := DecodeClient(readFixture(t, "client_profile_update_cursor_color.json")); err != nil {
		t.Fatal(err)
	}
	encoded := string(mustMarshalPayload(t, ParticipantPresence{ActorID: "a", DisplayName: "n", Sequence: 1, Status: "active"}))
	if strings.Contains(encoded, "cursor_color") {
		t.Fatalf("absent colour must be omitted: %s", encoded)
	}
}

func intPointer(value int) *int { return &value }

// APHELION EDIT ADDITION END

func TestContractsDeclareEveryFixtureMessage(t *testing.T) {
	t.Parallel()

	openAPI := readContract(t, "openapi.yaml")
	for _, route := range []string{
		"/v1/sessions:",
		"/v1/sessions/{session_id}:",
		"/v1/sessions/{session_id}/join-tokens:",
		"/v1/sessions/{session_id}/snapshot:",
		"/v1/sessions/{session_id}/exports:",
		"/v1/health/live:",
		"/v1/health/ready:",
		"/v1/version:",
	} {
		if !strings.Contains(openAPI, route) {
			t.Errorf("openapi.yaml missing %s", route)
		}
	}

	asyncAPI := readContract(t, "asyncapi.yaml")
	for _, messageType := range append(AllClientTypes(), AllServerTypes()...) {
		if !strings.Contains(asyncAPI, string(messageType)+":") {
			t.Errorf("asyncapi.yaml missing message %q", messageType)
		}
	}
}

func TestContractsAreValidPinnedYAMLDocuments(t *testing.T) {
	t.Parallel()

	for name, versionKey := range map[string]string{
		"openapi.yaml":  "openapi",
		"asyncapi.yaml": "asyncapi",
	} {
		name, versionKey := name, versionKey
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			var document map[string]any
			if err := yaml.Unmarshal([]byte(readContract(t, name)), &document); err != nil {
				t.Fatalf("pinned yaml.v3 validator rejected contract: %v", err)
			}
			if document[versionKey] == nil || document["info"] == nil || document["components"] == nil {
				t.Fatalf("contract lacks required %s, info, or components roots", versionKey)
			}
		})
	}
}

func TestOpenAPISnapshotLimitsMatchModel(t *testing.T) {
	t.Parallel()

	var document map[string]any
	if err := yaml.Unmarshal([]byte(readContract(t, "openapi.yaml")), &document); err != nil {
		t.Fatalf("decode openapi.yaml: %v", err)
	}
	components := requireYAMLMap(t, document, "components")
	schemas := requireYAMLMap(t, components, "schemas")
	snapshot := requireYAMLMap(t, schemas, "Snapshot")
	properties := requireYAMLMap(t, snapshot, "properties")
	for _, name := range []string{"max_x", "max_y", "max_z"} {
		dimension := requireYAMLMap(t, properties, name)
		if got := requireYAMLInt(t, dimension, "maximum"); got != model.MaxMapDimension {
			t.Errorf("Snapshot.%s maximum = %d, want %d", name, got, model.MaxMapDimension)
		}
	}
	tiles := requireYAMLMap(t, properties, "tiles")
	if got := requireYAMLInt(t, tiles, "maxItems"); got != model.MaxMapCells {
		t.Errorf("Snapshot.tiles maxItems = %d, want %d", got, model.MaxMapCells)
	}
}

func TestOpenAPIExportCheckpointContractMatchesHandler(t *testing.T) {
	t.Parallel()

	var document map[string]any
	if err := yaml.Unmarshal([]byte(readContract(t, "openapi.yaml")), &document); err != nil {
		t.Fatalf("decode openapi.yaml: %v", err)
	}
	paths := requireYAMLMap(t, document, "paths")
	exportPath := requireYAMLMap(t, paths, "/v1/sessions/{session_id}/exports")
	post := requireYAMLMap(t, exportPath, "post")
	responses := requireYAMLMap(t, post, "responses")
	for _, status := range []string{"202", "400", "403", "409", "413", "503"} {
		if _, exists := responses[status]; !exists {
			t.Errorf("export responses omit HTTP %s", status)
		}
	}
	if _, exists := responses["501"]; exists {
		t.Error("export responses still declare HTTP 501")
	}

	components := requireYAMLMap(t, document, "components")
	schemas := requireYAMLMap(t, components, "schemas")
	requestSchema := requireYAMLMap(t, schemas, "ExportRequest")
	required := requireYAMLStrings(t, requestSchema, "required")
	for _, name := range []string{"revision", "map_hash", "idempotency_key"} {
		if !slices.Contains(required, name) {
			t.Errorf("ExportRequest required fields omit %q", name)
		}
	}
	checkpointSchema := requireYAMLMap(t, schemas, "ExportCheckpoint")
	checkpointProperties := requireYAMLMap(t, checkpointSchema, "properties")
	status := requireYAMLMap(t, checkpointProperties, "status")
	if got := requireYAMLStrings(t, status, "enum"); !slices.Equal(got, []string{"pending", "accepted", "rejected"}) {
		t.Errorf("ExportCheckpoint status enum = %v", got)
	}
}

func requireYAMLMap(t *testing.T, values map[string]any, key string) map[string]any {
	t.Helper()
	value, ok := values[key].(map[string]any)
	if !ok {
		t.Fatalf("OpenAPI %s is %T, want object", key, values[key])
	}
	return value
}

func requireYAMLInt(t *testing.T, values map[string]any, key string) int {
	t.Helper()
	value, ok := values[key].(int)
	if !ok {
		t.Fatalf("OpenAPI %s is %T, want integer", key, values[key])
	}
	return value
}

func requireYAMLStrings(t *testing.T, values map[string]any, key string) []string {
	t.Helper()
	raw, ok := values[key].([]any)
	if !ok {
		t.Fatalf("OpenAPI %s is %T, want array", key, values[key])
	}
	result := make([]string, len(raw))
	for index, value := range raw {
		text, ok := value.(string)
		if !ok {
			t.Fatalf("OpenAPI %s[%d] is %T, want string", key, index, value)
		}
		result[index] = text
	}
	return result
}

func readFixture(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", "v1", name))
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func readContract(t *testing.T, name string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "..", "..", "..", "api", "collaboration", name))
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}
