package protocol

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func decodeFixtureStrict(t *testing.T, name string, value any) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", "v1", name))
	if err != nil {
		t.Fatal(err)
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(value); err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	return data
}

// The pre-descriptor listing must keep decoding and re-encode byte for byte:
// the new field is omitted when absent, so older strict peers never see it.
func TestHostedSessionsPageWithoutRepositoryIsUnchanged(t *testing.T) {
	var page HostedSessionsPage
	data := decodeFixtureStrict(t, "hosted_sessions_page.json", &page)
	if page.Sessions[0].Repository != nil {
		t.Fatal("absent repository decoded as present")
	}
	encoded, err := json.Marshal(page)
	if err != nil || !bytes.Equal(encoded, data) {
		t.Fatalf("re-encoding changed the legacy shape:\n%s\n%s", encoded, data)
	}
}

func TestHostedSessionsPageRepositoryFixture(t *testing.T) {
	var page HostedSessionsPage
	data := decodeFixtureStrict(t, "hosted_sessions_page_repository.json", &page)
	repository := page.Sessions[0].Repository
	if repository == nil || repository.Validate() != nil || repository.DMEName != "game.dme" || repository.GitBranch != "play-test" {
		t.Fatalf("repository = %+v", repository)
	}
	encoded, err := json.Marshal(page)
	if err != nil || !bytes.Equal(encoded, data) {
		t.Fatalf("round trip changed the descriptor shape:\n%s\n%s", encoded, data)
	}
	// Only the four documented descriptor fields exist.
	var raw struct {
		Sessions []struct {
			Repository map[string]json.RawMessage `json:"repository"`
		} `json:"sessions"`
	}
	if err := json.Unmarshal(data, &raw); err != nil || len(raw.Sessions[0].Repository) != 4 {
		t.Fatalf("descriptor fields = %v, %v", raw.Sessions[0].Repository, err)
	}
}
