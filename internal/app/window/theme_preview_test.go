package window_test

import (
	"image"
	"image/png"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/SpaiR/imgui-go"
	"github.com/go-gl/gl/v3.3-core/gl"
	"github.com/go-gl/glfw/v3.3/glfw"

	"sdmm/internal/aphelion/theme"
	"sdmm/internal/app/ui/uikit"
	"sdmm/internal/app/window"
	"sdmm/internal/imguiext/icon"
	"sdmm/internal/imguiext/style"
	w "sdmm/internal/imguiext/widget"
	"sdmm/internal/platform"
)

// TestThemePreview renders a representative editor layout (menu bar, docked
// panels, tree, toolbar, toggle, table, inputs, popup) in each theme and
// writes PNGs to APHELION_THEME_PREVIEW_DIR. It is a design-review aid, not a
// gate: it only runs when that directory is set.
func TestThemePreview(t *testing.T) {
	dir := os.Getenv("APHELION_THEME_PREVIEW_DIR")
	if dir == "" || lifecycleWindow == nil {
		t.Skip("set APHELIONDMM_GL_TEST=1 and APHELION_THEME_PREVIEW_DIR")
	}
	runtime.LockOSThread()
	t.Cleanup(runtime.UnlockOSThread)
	lifecycleWindow.MakeContextCurrent()
	t.Cleanup(glfw.DetachCurrentContext)

	const width, height = 1440, 860
	var fbo, tex uint32
	gl.GenFramebuffers(1, &fbo)
	gl.GenTextures(1, &tex)
	gl.BindTexture(gl.TEXTURE_2D, tex)
	gl.TexImage2D(gl.TEXTURE_2D, 0, gl.RGBA, width, height, 0, gl.RGBA, gl.UNSIGNED_BYTE, nil)
	gl.BindFramebuffer(gl.FRAMEBUFFER, fbo)
	gl.FramebufferTexture2D(gl.FRAMEBUFFER, gl.COLOR_ATTACHMENT0, gl.TEXTURE_2D, tex, 0)
	t.Cleanup(func() {
		gl.BindFramebuffer(gl.FRAMEBUFFER, 0)
		gl.DeleteFramebuffers(1, &fbo)
		gl.DeleteTextures(1, &tex)
	})

	for _, name := range theme.Names {
		ctx := imgui.CreateContext(nil)
		io := imgui.CurrentIO()
		io.SetIniFilename("")
		io.SetConfigFlags(imgui.ConfigFlagsDockingEnable)
		io.SetDisplaySize(imgui.Vec2{X: width, Y: height})
		io.SetDisplayFrameBufferScale(imgui.Vec2{X: 1, Y: 1})
		io.SetDeltaTime(1.0 / 60)
		platform.InitImGuiGL()
		window.SetPointSize(1)
		window.SetTheme(name)

		for frame := 0; frame < 4; frame++ {
			imgui.NewFrame()
			n := window.PushThemeMetrics()
			previewLayout(frame == 0)
			imgui.PopStyleVarV(n)
			imgui.Render()
			gl.Viewport(0, 0, width, height)
			bg := imgui.CurrentStyle().Color(imgui.StyleColorWindowBg)
			gl.ClearColor(bg.X, bg.Y, bg.Z, 1)
			gl.Clear(gl.COLOR_BUFFER_BIT)
			platform.Render(imgui.RenderedDrawData())
		}
		pixels := make([]byte, width*height*4)
		gl.ReadPixels(0, 0, width, height, gl.RGBA, gl.UNSIGNED_BYTE, gl.Ptr(pixels))
		img := image.NewRGBA(image.Rect(0, 0, width, height))
		for y := 0; y < height; y++ { // GL rows are bottom-up
			copy(img.Pix[y*width*4:(y+1)*width*4], pixels[(height-1-y)*width*4:(height-y)*width*4])
		}
		for i := 3; i < len(img.Pix); i += 4 {
			img.Pix[i] = 255
		}
		f, err := os.Create(filepath.Join(dir, "theme-"+name+".png"))
		if err != nil {
			t.Fatal(err)
		}
		if err := png.Encode(f, img); err != nil {
			t.Fatal(err)
		}
		_ = f.Close()
		platform.DisposeImGuiGL()
		ctx.Destroy()
	}
	window.SetTheme(theme.NameMeridian)
}

func previewLayout(first bool) {
	w.MainMenuBar(w.Layout{
		w.Menu("File", nil), w.Menu("Edit", nil), w.Menu("Collaboration", nil), w.Menu("View", nil), w.Menu("Window", nil), w.Menu("Help", nil),
		w.Custom(func() {
			if theme.IsMeridian() {
				pos, size := imgui.WindowPos(), imgui.WindowSize()
				theme.DrawSpectrumStripe(imgui.WindowDrawList(), imgui.Vec2{X: pos.X, Y: pos.Y + size.Y - 2}, imgui.Vec2{X: pos.X + size.X, Y: pos.Y + size.Y})
			}
		}),
	}).Build()

	dock := imgui.DockSpaceOverViewportV(imgui.MainViewport(), imgui.DockNodeFlagsNone)
	if first {
		var left, center, right, rightUp, rightDown int32
		center = int32(dock)
		imgui.DockBuilderRemoveNode(dock)
		imgui.DockBuilderAddNodeV(dock, imgui.DockNodeFlagsDockSpace)
		imgui.DockBuilderSetNodeSize(int(center), imgui.MainViewport().Size())
		imgui.DockBuilderSplitNode(int(center), imgui.DirLeft, .22, &left, &center)
		imgui.DockBuilderSplitNode(int(center), imgui.DirRight, .27, &right, &center)
		imgui.DockBuilderSplitNode(int(right), imgui.DirUp, .5, &rightUp, &rightDown)
		for _, n := range []string{"Environment", "Map Lint", "Playtest"} {
			imgui.DockBuilderDockWindow(n, int(left))
		}
		for _, n := range []string{"Prefabs", "Search", "Mapping Helpers"} {
			imgui.DockBuilderDockWindow(n, int(rightUp))
		}
		imgui.DockBuilderDockWindow("Variables", int(rightDown))
		imgui.DockBuilderDockWindow("MiniStation.dmm", int(center))
		imgui.DockBuilderFinish(dock)
	}

	panel := func(name string, body func()) {
		if imgui.Begin(name) {
			body()
		}
		imgui.End()
	}
	panel("Map Lint", func() {})
	panel("Playtest", func() {})
	panel("Environment", func() {
		for _, b := range []string{icon.FilterAlt + " Profiles", icon.Eye + " Show All", "Unhide Last", icon.Undo, icon.Redo} {
			imgui.Button(b)
			imgui.SameLine()
		}
		imgui.NewLine()
		imgui.TextDisabled("Visibility 2/2: Hide subtree: /area")
		filter := ""
		imgui.SetNextItemWidth(-1)
		imgui.InputTextWithHint("##filter", "Filter", &filter)
		imgui.SetNextItemOpen(true, imgui.ConditionAlways)
		if imgui.TreeNodeV("light", imgui.TreeNodeFlagsSpanAvailWidth) {
			for i, n := range []string{"blacklight", "broken", "burned", "cold", "dim", "directional", "empty"} {
				imgui.SelectableV(n, i == 2, 0, imgui.Vec2{})
			}
			imgui.TreePop()
		}
		for _, n := range []string{"light_switch", "limbgrower", "loot_locator", "mailsorter"} {
			imgui.TreeNodeV(n, imgui.TreeNodeFlagsLeaf|imgui.TreeNodeFlagsNoTreePushOnOpen)
		}
	})
	panel("MiniStation.dmm", func() {
		for _, b := range []string{icon.Add, icon.BorderAll, icon.EyeDropper, icon.Eraser, icon.Repeat} {
			imgui.Button(b)
			imgui.SameLine()
		}
		w.Button(icon.Wrench+" Brush", nil).Style(style.ButtonGold{}).TextColor(style.ColorBlack).Build()
		imgui.SameLine()
		imgui.Button("Options")
		imgui.TextDisabled("Lighting (approximate): 539 sources, 20 skipped")
		if imgui.BeginChildV("viewport", imgui.Vec2{X: -1, Y: -imgui.FrameHeightWithSpacing()}, true, 0) {
			uikit.EmptyState("map viewport")
		}
		imgui.EndChild()
		// The segmented status bar, as pmap draws it.
		imgui.AlignTextToFramePadding()
		segments := []struct {
			text string
			mono bool
		}{{"X:126 Y:127", true}, {"Move tile · Move instance  [Ctrl]", false}, {"/obj/machinery/door/airlock/public/glass", true}, {"3×2", true}}
		for i, s := range segments {
			if i > 0 {
				imgui.SameLine()
				imgui.TextDisabled("|")
				imgui.SameLine()
			}
			if s.mono {
				uikit.MonoText(s.text)
			} else {
				imgui.Text(s.text)
			}
		}
	})
	panel("Search", func() {})
	panel("Mapping Helpers", func() {})
	panel("Prefabs", func() {
		for i, d := range []string{"dir = 1; light_color = \"#d1dfff\"", "dir = 4; light_color = \"#c1caff\"", "dir = 1; light_power = 0; bulb_colour = \"#FF0000\"; bulb_power = 5"} {
			imgui.SelectableV("light fixture##"+d, i == 1, 0, imgui.Vec2{})
			imgui.SameLine()
			uikit.MonoText(uikit.EndTruncate(d, imgui.ContentRegionAvail().X, func(s string) float32 { return imgui.CalcTextSize(s, false, 0).X }))
		}
	})
	panel("Variables", func() {
		if imgui.BeginTableV("mode", 2, imgui.TableFlagsNoPadInnerX, imgui.Vec2{}, 0) {
			imgui.TableNextColumn()
			w.Button("Instance", nil).Style(style.ButtonSelected{}).Size(imgui.Vec2{X: -1}).Build()
			imgui.TableNextColumn()
			w.Button("Prefab", nil).Style(style.ButtonDefault{}).Size(imgui.Vec2{X: -1}).Build()
			imgui.EndTable()
		}
		// As cpvareditor: the controls sit above the list, outside its scroll.
		w.Button(icon.Search, nil).Round(true).Build()
		imgui.SameLine()
		w.Button(icon.Cog, nil).Round(true).Build()
		imgui.SameLine()
		filter := "bulb"
		imgui.SetNextItemWidth(-1)
		imgui.InputTextWithHint("##var_filter", "Filter", &filter)
		imgui.BeginChild("variables")
		defer imgui.EndChild()
		uikit.SectionLabel("Pinned")
		if imgui.BeginTableV("vars", 2, imgui.TableFlagsRowBg|imgui.TableFlagsBordersInnerV, imgui.Vec2{}, 0) {
			for i, row := range [][2]string{{"bulb_colour", `"#FF0000"`}, {"bulb_power", "0.9"}, {"brightness", "7.5"}, {"dir", "1"}, {"name", `"light fixture"`}} {
				imgui.TableNextColumn()
				imgui.AlignTextToFramePadding()
				if i < 2 {
					imgui.TextColored(style.ColorGreen3, row[0])
				} else {
					imgui.Text(row[0])
				}
				imgui.TableNextColumn()
				v := row[1]
				uikit.Mono(func() {
					imgui.SetNextItemWidth(-1)
					imgui.InputText("##"+row[0], &v)
				})
			}
			imgui.EndTable()
		}
		uikit.SectionLabel("Other")
		for _, name := range []string{"anchored", "base_state", "bulb_emergency_colour", "bulb_low_power_colour", "density", "desc", "fire_brightness"} {
			imgui.AlignTextToFramePadding()
			imgui.Text(name)
		}
	})
}
