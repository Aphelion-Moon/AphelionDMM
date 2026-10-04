package psettings

import (
	"image/png"
	"os"
	"path/filepath"
	"testing"
)

func TestScreenshotSavePinsDestinationBeforeWorkerStarts(t *testing.T) {
	previous := cfg
	t.Cleanup(func() { cfg = previous })
	destination, other := t.TempDir(), t.TempDir()
	cfg = &psettingsConfig{ScreenshotDir: destination}
	work := (&Panel{}).prepareScreenshotSave([]byte{10, 20, 30, 255}, 1, 1)
	cfg.ScreenshotDir = other
	if err := work(); err != nil {
		t.Fatal(err)
	}
	files, err := filepath.Glob(filepath.Join(destination, "*.png"))
	if err != nil || len(files) != 1 {
		t.Fatal("screenshot followed later settings instead of captured destination", files, err)
	}
	file, err := os.Open(files[0])
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = file.Close() }()
	decoded, err := png.Decode(file)
	if err != nil || decoded.Bounds().Dx() != 1 || decoded.Bounds().Dy() != 1 {
		t.Fatal("invalid exported pixels", err)
	}
	files, err = filepath.Glob(filepath.Join(other, "*.png"))
	if err != nil || len(files) != 0 {
		t.Fatal("changed destination received screenshot", files, err)
	}
}

func TestScreenshotCompletionReturnsToUIAndOriginalSession(t *testing.T) {
	jobs := make(chan func(), 1)
	session := &sessionScreenshot{saving: true}
	panel := &Panel{sessionScreenshot: session}
	done := make(chan struct{})
	go func() { completeScreenshotSave(func(job func()) { jobs <- job }, session, nil); close(done) }()
	<-done
	if !session.saving {
		t.Fatal("worker mutated UI state before dispatch")
	}
	replacement := &sessionScreenshot{saving: true}
	panel.sessionScreenshot = replacement
	(<-jobs)()
	if session.saving || !replacement.saving {
		t.Fatal("completion changed the wrong screenshot session")
	}
}
