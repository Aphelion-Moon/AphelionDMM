package app

import (
	"runtime"
	"testing"
	"unsafe"

	"github.com/SpaiR/imgui-go"

	"sdmm/internal/app/ui/dialog"
)

func TestRepeatedCloseRequestDoesNotQueueAnotherPrompt(t *testing.T) {
	var prompt closePrompt
	var callbacks []func(bool)
	requests := 0
	request := func(done func(bool)) {
		requests++
		callbacks = append(callbacks, done)
		dialog.Open(dialog.TypeConfirmation{Title: "Save Workspace?"})
	}
	var result []bool
	done := func(closed bool) { result = append(result, closed) }
	t.Cleanup(func() {
		for dialog.IsOpen("Save Workspace?") {
			dialog.Close(dialog.TypeConfirmation{Title: "Save Workspace?"})
		}
	})

	prompt.track(request, done)
	for range 4 {
		if !prompt.pending() {
			t.Fatal("open save prompt is not reported as pending")
		}
	}
	if requests != 1 {
		t.Fatalf("close requests issued %d prompts, want 1", requests)
	}

	// Answering clears the pending prompt and reports the result once.
	callbacks[0](false)
	dialog.Close(dialog.TypeConfirmation{Title: "Save Workspace?"})
	if prompt.pending() || len(result) != 1 || result[0] {
		t.Fatalf("answered prompt pending=%v results=%v", prompt.pending(), result)
	}

	// A prompt dismissed without an answer must not block a later close.
	prompt.track(request, done)
	dialog.Close(dialog.TypeConfirmation{Title: "Save Workspace?"})
	if prompt.pending() {
		t.Fatal("dismissed prompt still blocks closing")
	}
}

func TestCloseRequestWithoutPromptCompletesImmediately(t *testing.T) {
	var prompt closePrompt
	closed := false
	prompt.track(func(done func(bool)) { done(true) }, func(value bool) { closed = value })
	if !closed || prompt.pending() || prompt.name != "" {
		t.Fatal("synchronous close was treated as a pending prompt")
	}
}

// A balanced but nested BeginDisabled leaks reduced alpha in the pinned ImGui.
// The application frame scope must restore full opacity for the next frame.
func TestFrameAlphaScopeDiscardsDisabledAlphaLeak(t *testing.T) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	ctx := imgui.CreateContext(nil)
	defer ctx.Destroy()
	io := imgui.CurrentIO()
	io.SetIniFilename("")
	io.SetDisplaySize(imgui.Vec2{X: 400, Y: 300})
	io.SetDeltaTime(1.0 / 60)
	io.Fonts().TextureDataRGBA32()
	for frame := range 4 {
		imgui.NewFrame()
		pushFrameAlpha()
		imgui.Begin("Probe")
		imgui.TextColored(imgui.Vec4{X: 1, Y: 1, Z: 1, W: 1}, "Legible text")
		vertices, size := imgui.WindowDrawList().VertexBuffer()
		stride, _, _, colorOffset := imgui.VertexBufferLayout()
		alpha := unsafe.Slice((*byte)(vertices), size)[size-stride+colorOffset+3]
		imgui.BeginDisabled()
		imgui.BeginDisabled()
		imgui.EndDisabled()
		imgui.EndDisabled()
		imgui.End()
		popFrameAlpha()
		imgui.Render()
		if alpha != 255 {
			t.Fatalf("frame %d began with a leaked alpha: %d, want 255", frame, alpha)
		}
	}
}
