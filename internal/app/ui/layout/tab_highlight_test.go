package layout

import (
	"runtime"
	"testing"

	"github.com/SpaiR/imgui-go"

	"sdmm/internal/app/ui/component"
)

type highlightNode struct {
	component.Component
	on bool
}

func (n *highlightNode) TabHighlight() bool { return n.on }

func TestPushTabHighlightTintsOnlyWhenAsked(t *testing.T) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	ctx := imgui.CreateContext(nil)
	defer ctx.Destroy()
	base := imgui.CurrentStyle().Color(imgui.StyleColorTab)

	if n := pushTabHighlight(&highlightNode{}); n != 0 {
		t.Fatalf("pushed %d colours for a quiet tab", n)
	}
	if n := pushTabHighlight(&component.Component{}); n != 0 {
		t.Fatalf("pushed %d colours for a node without highlighting", n)
	}
	n := pushTabHighlight(&highlightNode{on: true})
	if n == 0 {
		t.Fatal("a highlighted tab pushed no colours")
	}
	if tinted := imgui.CurrentStyle().Color(imgui.StyleColorTab); tinted == base {
		t.Fatal("tab colour unchanged")
	}
	imgui.PopStyleColorV(n)
	if restored := imgui.CurrentStyle().Color(imgui.StyleColorTab); restored != base {
		t.Fatal("tab colour not restored")
	}
}
