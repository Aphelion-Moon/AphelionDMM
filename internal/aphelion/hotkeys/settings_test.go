package hotkeys

import (
	"encoding/json"
	"reflect"
	"sync"
	"testing"

	"github.com/go-gl/glfw/v3.3/glfw"
)

func TestEditableBindingsParseAndRoundTrip(t *testing.T) {
	chords, err := Parse("Ctrl+Shift+K; Alt+1/Numpad 1")
	if err != nil {
		t.Fatal(err)
	}
	if len(chords) != 2 || chords[0][0] != [2]glfw.Key{glfw.KeyLeftControl, glfw.KeyRightControl} {
		t.Fatalf("unexpected chords: %v", chords)
	}
	var settings Settings
	if err := settings.Set("menu#DoSave", chords); err != nil {
		t.Fatal(err)
	}
	chords[0][0][0] = glfw.KeyF12
	data, err := json.Marshal(&settings)
	if err != nil {
		t.Fatal(err)
	}
	var reopened Settings
	if err := json.Unmarshal(data, &reopened); err != nil {
		t.Fatal(err)
	}
	got, ok := reopened.Lookup("menu#DoSave")
	parsed, err := Parse(Format(got))
	if !ok || err != nil || !reflect.DeepEqual(parsed, got) || got[0][0][0] != glfw.KeyLeftControl {
		t.Fatalf("round trip lost bindings: %v, %v", got, err)
	}
	got[0][0][0] = glfw.KeyF12
	again, _ := reopened.Lookup("menu#DoSave")
	if again[0][0][0] != glfw.KeyLeftControl {
		t.Fatal("lookup exposes mutable settings")
	}
	reopened.Reset("menu#DoSave")
	if _, ok := reopened.Lookup("menu#DoSave"); ok {
		t.Fatal("reset kept override")
	}
	if err := reopened.Set("menu#DoSave", nil); err != nil {
		t.Fatal(err)
	}
	if disabled, ok := reopened.Lookup("menu#DoSave"); !ok || len(disabled) != 0 {
		t.Fatal("empty binding must disable action")
	}
	reopened.ResetAll()
	if _, ok := reopened.Lookup("menu#DoSave"); ok {
		t.Fatal("reset all kept disabled action")
	}
}

func TestBindingValidationAndInvalidSavedFallback(t *testing.T) {
	for _, text := range []string{"Ctrl", "Ctrl+Ctrl+K", "K+Ctrl", "Ctrl+Unknown", "Ctrl+", "S", "Alt+D", "Shift+R", "Space", "Ctrl+Space", "Esc"} {
		if _, err := Parse(text); err == nil {
			t.Errorf("accepted unsafe or invalid chord %q", text)
		}
	}
	var settings Settings
	if err := json.Unmarshal([]byte(`{"menu#good":[[[341,345],[75,0]]],"menu#invalid":[[[999999,0]]],"future#unknown":[],"menu#badShape":"bad"}`), &settings); err != nil {
		t.Fatal(err)
	}
	if _, ok := settings.Lookup("menu#good"); !ok {
		t.Fatal("valid binding lost")
	}
	if _, ok := settings.Lookup("menu#invalid"); ok {
		t.Fatal("invalid key suppressed defaults")
	}
	if _, ok := settings.Lookup("menu#badShape"); ok {
		t.Fatal("invalid shape suppressed defaults")
	}
	if _, ok := settings.Lookup("future#unknown"); !ok {
		t.Fatal("unregistered action lost before its pane opened")
	}
}

func TestSettingsConcurrentPreferenceSerialization(t *testing.T) {
	var settings Settings
	chords, err := Parse("Ctrl+K")
	if err != nil {
		t.Fatal(err)
	}
	var workers sync.WaitGroup
	workers.Add(2)
	go func() {
		defer workers.Done()
		for range 100 {
			_ = settings.Set("menu#save", chords)
			settings.Reset("menu#save")
		}
	}()
	go func() {
		defer workers.Done()
		for range 100 {
			if _, err := json.Marshal(&settings); err != nil {
				t.Error(err)
			}
			settings.Lookup("menu#save")
		}
	}()
	workers.Wait()
}
