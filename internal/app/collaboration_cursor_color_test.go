package app

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	collabui "sdmm/internal/aphelion/collab/ui"
)

func loadColorApp(t *testing.T, dir, collaboration string) *app {
	t.Helper()
	body := `{"Version":3,"Editor":{"SaveFormat":"TGM","NudgeMode":"step_x/step_y"}` + collaboration + `}`
	if err := os.WriteFile(filepath.Join(dir, "preferences.json"), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	a := &app{configDir: dir}
	a.loadPreferencesConfig()
	return a
}

func TestCollaborationCursorColorPreferenceLoadsAndNormalizes(t *testing.T) {
	for name, test := range map[string]struct {
		json string
		want int // -1 means unset
	}{
		"absent":       {"", -1},
		"null":         {`,"Collaboration":{"CursorColor":null}`, -1},
		"valid":        {`,"Collaboration":{"CursorColor":3}`, 3},
		"zero":         {`,"Collaboration":{"CursorColor":0}`, 0},
		"out of range": {`,"Collaboration":{"CursorColor":99}`, -1},
		"negative":     {`,"Collaboration":{"CursorColor":-2}`, -1},
	} {
		t.Run(name, func(t *testing.T) {
			a := loadColorApp(t, t.TempDir(), test.json)
			got := a.CollaborationCursorColor()
			if test.want == -1 && got != nil || test.want != -1 && (got == nil || *got != test.want) {
				t.Fatalf("CollaborationCursorColor() = %v, want %d", got, test.want)
			}
		})
	}
}

func TestSetCollaborationCursorColorPersistsAndReachesClient(t *testing.T) {
	dir := t.TempDir()
	a := loadColorApp(t, dir, "")
	a.collaborationClient = collabui.NewSessionClient(collabui.SessionClientConfig{})
	a.DoSetCollaborationCursorColor(99) // ignored: outside the palette
	if a.CollaborationCursorColor() != nil {
		t.Fatal("invalid colour was stored")
	}
	a.DoSetCollaborationCursorColor(5)
	deadline := time.Now().Add(2 * time.Second)
	for {
		if color := a.collaborationClient.CursorColor(); color != nil && *color == 5 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("colour never reached the session client")
		}
		time.Sleep(5 * time.Millisecond)
	}
	reopened := &app{configDir: dir}
	reopened.loadPreferencesConfig()
	if got := reopened.CollaborationCursorColor(); got == nil || *got != 5 {
		t.Fatalf("persisted colour = %v, want 5", got)
	}
	reopened.collaborationClient = collabui.NewSessionClient(collabui.SessionClientConfig{})
	reopened.seedCollaborationCursorColor()
	if color := reopened.collaborationClient.CursorColor(); color == nil || *color != 5 {
		t.Fatalf("seeded client colour = %v", color)
	}
}
