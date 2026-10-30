package app

import (
	"github.com/SpaiR/imgui-go"

	"sdmm/internal/app/ui/dialog"
)

// closePrompt remembers the save confirmation an application close request
// left open, so pressing the window close button again while it is pending
// does not queue another identical prompt.
type closePrompt struct {
	name string
}

// pending reports whether an earlier close request is still waiting on its
// prompt. A prompt dismissed without answering no longer blocks closing.
func (p *closePrompt) pending() bool {
	return p.name != "" && dialog.IsOpen(p.name)
}

// track runs request with a completion that forwards to done. When request
// returns without completing, it has opened a prompt, which is remembered.
func (p *closePrompt) track(request func(done func(closed bool)), done func(closed bool)) {
	answered := false
	request(func(closed bool) {
		answered = true
		p.name = ""
		done(closed)
	})
	if !answered {
		p.name, _ = dialog.Newest()
	}
}

// The pinned ImGui keeps one disabled-alpha backup that nested BeginDisabled
// calls overwrite, leaving Style.Alpha reduced after balanced calls. Scoping
// full opacity to each application frame keeps such a leak from compounding
// across frames until every panel and dialog is invisible.
func pushFrameAlpha() { imgui.PushStyleVarFloat(imgui.StyleVarAlpha, 1) }

func popFrameAlpha() { imgui.PopStyleVar() }
