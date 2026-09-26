package mappingui

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/SpaiR/imgui-go"
	"sdmm/internal/aphelion/mapping"
	"sdmm/internal/aphelion/resources"
)

type occurrenceRow struct {
	root     mapping.Root
	depth    int
	children bool
}

func (p *Panel) rebuildTree() {
	if !p.treeDirty && p.treeFilter == p.rootFilter {
		return
	}
	p.treeDirty, p.treeFilter = false, p.rootFilter
	p.treeRows = nil
	if p.current == nil {
		return
	}
	roots := map[string]mapping.Root{}
	children := map[string][]mapping.Root{}
	for _, root := range p.current.roots {
		roots[root.ID] = root
	}
	for _, root := range p.current.roots {
		parent := root.Parent
		if _, exists := roots[parent]; !exists {
			parent = ""
		}
		children[parent] = append(children[parent], root)
	}
	filter := strings.ToLower(p.rootFilter)
	include := map[string]bool{}
	for _, root := range p.current.roots {
		if filter == "" || strings.Contains(strings.ToLower(root.Key+" "+root.Source.Path), filter) {
			for id := root.ID; id != "" && !include[id]; id = roots[id].Parent {
				include[id] = true
			}
		}
	}
	if p.revealRoot {
		for id := roots[p.focusRoot].Parent; id != ""; id = roots[id].Parent {
			if p.expanded[id] {
				break
			}
			p.expanded[id] = true
		}
	}
	visited := map[string]bool{}
	var add func(string, int)
	add = func(parent string, depth int) {
		for _, root := range children[parent] {
			if visited[root.ID] || !include[root.ID] {
				continue
			}
			visited[root.ID] = true
			p.treeRows = append(p.treeRows, occurrenceRow{root, depth, len(children[root.ID]) != 0})
			if p.expanded[root.ID] || filter != "" {
				add(root.ID, depth+1)
			}
		}
	}
	add("", 0)
}

func (p *Panel) sidebar() {
	imgui.TextWrapped(filepath.Base(p.parentPath))
	if imgui.IsItemHovered() {
		imgui.SetTooltip(p.parentPath)
	}
	if imgui.Checkbox("Show preview", &p.previewVisible) {
		p.setPreviewVisible(p.previewVisible)
	}
	if imgui.Button("More...") {
		imgui.OpenPopup("composition-more")
	}
	if imgui.BeginPopup("composition-more") {
		if host, ok := p.app.(mapHost); ok && imgui.Button("Open comparison tab") {
			p.open = true
			host.OpenMappingComparison(p)
			if p.current == nil && p.pending == nil && p.results == nil {
				p.compose = true
				p.queue(nil)
			}
		}
		imgui.BeginChildV("advanced", imgui.Vec2{X: min(560, imgui.MainViewport().Size().X-80), Y: min(520, imgui.MainViewport().Size().Y-120)}, false, imgui.WindowFlagsNone)
		if p.current != nil && imgui.TreeNode(fmt.Sprintf("Diagnostics (%d)", len(p.current.diagnostics))) {
			for _, d := range p.current.diagnostics {
				imgui.TextWrapped(d.Severity + ": " + d.Message)
			}
			imgui.TreePop()
		}
		if imgui.TreeNode("References and authoring") {
			p.controls()
			imgui.TreePop()
		}
		imgui.EndChild()
		imgui.EndPopup()
	}
	// A fixed status region prevents pending/error text from moving navigation.
	imgui.BeginChildV("preview-status", imgui.Vec2{Y: imgui.TextLineHeightWithSpacing() * 3}, false, imgui.WindowFlagsNone)
	p.previewStatus()
	imgui.EndChild()
	if p.current != nil && p.mapConfig == "" && len(p.current.configurations) > 1 {
		if imgui.BeginCombo("Configuration", "Choose configuration") {
			for _, config := range p.current.configurations {
				if imgui.Selectable(config) {
					p.mapConfig = config
					p.queue(nil)
				}
			}
			imgui.EndCombo()
		}
	}
	imgui.SetNextItemWidth(-1)
	imgui.InputText("##find-placement", &p.rootFilter)
	if imgui.IsItemHovered() {
		imgui.SetTooltip("Search placements")
	}
	p.rebuildTree()
	available := imgui.ContentRegionAvail().Y
	if p.treeHeight == 0 {
		p.treeHeight = max(imgui.TextLineHeight()*3, available*.30)
	}
	height := max(imgui.TextLineHeight()*3, min(p.treeHeight, max(imgui.TextLineHeight()*3, available-imgui.TextLineHeight()*9)))
	imgui.BeginChildV("occurrences", imgui.Vec2{Y: height}, true, imgui.WindowFlagsNone)
	rowHeight := imgui.TextLineHeightWithSpacing()
	// Rows have constant height; clipping keeps inspection independent of tree size.
	first := max(0, int(imgui.ScrollY()/rowHeight)-1)
	last := min(len(p.treeRows), first+int(height/rowHeight)+3)
	if p.revealRoot {
		for i, row := range p.treeRows {
			if row.root.ID == p.focusRoot {
				imgui.SetScrollY(float32(i) * rowHeight)
				first = max(0, i-1)
				last = min(len(p.treeRows), first+int(height/rowHeight)+3)
				p.revealRoot = false
				break
			}
		}
	}
	if first > 0 {
		imgui.Dummy(imgui.Vec2{Y: float32(first)*rowHeight - imgui.CurrentStyle().ItemSpacing().Y})
	}
	for i := first; i < last; i++ {
		row := p.treeRows[i]
		imgui.PushID(row.root.ID)
		if row.depth > 0 {
			imgui.IndentV(float32(row.depth) * imgui.FontSize())
		}
		if row.children {
			arrow := ">"
			if p.expanded[row.root.ID] || p.rootFilter != "" {
				arrow = "v"
			}
			if imgui.SmallButton(arrow) {
				p.expanded[row.root.ID] = !p.expanded[row.root.ID]
				p.treeDirty = true
			}
			imgui.SameLine()
		}
		label := fmt.Sprintf("%s  Z%d", row.root.Key, row.root.Destination.Z)
		if imgui.SelectableV(label, row.root.ID == p.focusRoot, imgui.SelectableFlagsAllowDoubleClick, imgui.Vec2{}) {
			p.selectOccurrence(row.root.ID)
			if imgui.IsMouseDoubleClicked(0) {
				p.navigate(row.root.Destination)
			}
		}
		if imgui.IsItemHovered() {
			imgui.SetTooltip(label + "\n" + row.root.Source.Path)
		}
		if imgui.BeginPopupContextItem() {
			if p.focusRoot != row.root.ID {
				p.selectOccurrence(row.root.ID)
			}
			p.placementActions(row.root)
			imgui.EndPopup()
		}
		if row.depth > 0 {
			imgui.UnindentV(float32(row.depth) * imgui.FontSize())
		}
		imgui.PopID()
	}
	if last < len(p.treeRows) {
		imgui.Dummy(imgui.Vec2{Y: float32(len(p.treeRows)-last) * rowHeight})
	}
	imgui.EndChild()
	imgui.InvisibleButton("resize-occurrences", imgui.Vec2{X: -1, Y: 5})
	if imgui.IsItemActive() {
		p.treeHeight = max(imgui.TextLineHeight()*3, height+imgui.CurrentIO().MouseDelta().Y)
	}
	imgui.BeginChildV("selection-inspector", imgui.Vec2{}, false, imgui.WindowFlagsNone)
	defer imgui.EndChild()
	if len(p.contributors) > 1 {
		imgui.TextWrapped("Choose the occurrence at this tile:")
		for _, id := range p.contributors {
			for _, root := range p.current.roots {
				if root.ID == id && imgui.Selectable(root.Key+" / "+filepath.Base(root.Source.Path)) {
					p.selectOccurrence(id)
					p.contributors = nil
				}
			}
		}
	}
	root, ok := p.selectedRoot()
	if !ok {
		imgui.TextWrapped("Select a placement to inspect it.")
		return
	}
	imgui.TextWrapped(root.Key)
	placement, placed := p.selectedPlacement()
	displayed := "Unavailable"
	if placed {
		displayed = filepath.Base(placement.Source.Path)
	}
	imgui.TextWrapped("Displayed source: " + displayed)
	imgui.SetNextItemWidth(-1)
	if imgui.BeginCombo("##alternative", displayed) {
		for _, candidate := range root.Candidates {
			label := filepath.Base(candidate.Path)
			if placed && candidate.Slot == placement.Slot {
				label += " (Displayed)"
			}
			imgui.BeginDisabledV(candidate.Error != "")
			if imgui.Selectable(label) {
				p.chooseAlternative(root, candidate)
			}
			imgui.EndDisabled()
			if candidate.Error != "" && imgui.IsItemHovered() {
				imgui.SetTooltip(candidate.Error)
			}
		}
		imgui.EndCombo()
	}
	if imgui.Button("Locate") {
		p.navigate(root.Destination)
	}
	enabled := placed && p.choiceFulfilled(root.ID) && !p.stale && p.pending == nil && p.results == nil && p.operation.err == nil
	imgui.BeginDisabledV(!enabled)
	editLabel := "Edit source in context"
	if imgui.CalcTextSize(editLabel, false, 0).X+imgui.CurrentStyle().FramePadding().X*2 > imgui.ContentRegionAvail().X {
		editLabel = "Edit source\nin context"
	}
	if imgui.ButtonV(editLabel, imgui.Vec2{X: -1}) {
		p.openSelectedSource()
	}
	imgui.EndDisabled()
	if !enabled {
		imgui.TextWrapped("Source unavailable or preview not current. Wait, retry, or dismiss the requested change.")
	}
	if placed {
		count := 0
		for _, other := range p.current.projection.Placements {
			if viewKey(other.Source.Path) == viewKey(placement.Source.Path) {
				count++
			}
		}
		imgui.TextWrapped(fmt.Sprintf("Used %d times in this preview", count))
	}
	imgui.TextWrapped(fmt.Sprintf("Location: %d, %d · Z%d", root.Destination.X, root.Destination.Y, root.Destination.Z))
	if int(p.level) != root.Destination.Z {
		imgui.TextWrapped("Selected placement is on another deck.")
	}
	if imgui.Button("Placement actions...") {
		imgui.OpenPopup("placement-actions")
	}
	if imgui.BeginPopup("placement-actions") {
		p.placementActions(root)
		imgui.EndPopup()
	}
	if p.sourceStatus != "" {
		imgui.TextWrapped(p.sourceStatus)
	}
	if p.status != "" {
		imgui.TextWrapped(p.status)
	}
}

func (p *Panel) placementActions(root mapping.Root) {
	if imgui.Button("Locate") {
		p.navigate(root.Destination)
	}
	imgui.BeginDisabledV(p.stale || p.pending != nil || p.results != nil || p.operation.err != nil || !p.choiceFulfilled(root.ID))
	if root.Parent != "" && imgui.Button("Edit containing source") {
		p.enterContainingSource(root)
	}
	imgui.EndDisabled()
	imgui.BeginDisabledV(p.stale || p.pending != nil || p.results != nil || p.operation.err != nil || !p.choiceFulfilled(root.ID))
	if root.Parent == "" && root.StableID != "" && imgui.Button("Move anchor") {
		p.armAnchorMove()
	}
	if root.Parent == "" && root.StableID != "" && imgui.TreeNode("Move anchor to another deck") {
		if p.targetDeck < 1 {
			p.targetDeck = int32(root.Local.Z)
		}
		imgui.InputInt("Destination deck", &p.targetDeck)
		if imgui.Button("Commit deck move") {
			if host, ok := p.app.(mapHost); ok {
				to := root.Local
				to.Z = int(p.targetDeck)
				if err := host.MoveMappingRoot(root, to, false); err != nil {
					p.status = err.Error()
				} else {
					p.status = "Deck move submitted"
				}
			}
		}
		imgui.TreePop()
	}
	imgui.EndDisabled()
	p.alternativeControls(root)
	excluded := p.scenario.Excluded[root.ID]
	if imgui.Checkbox("Exclude (editor scenario)", &excluded) {
		if p.scenario.Excluded == nil {
			p.scenario.Excluded = map[string]bool{}
		}
		p.scenario.Excluded[root.ID] = excluded
		p.queue(nil)
	}
	if imgui.TreeNode("Source and binding details") {
		imgui.TextWrapped(root.Source.Path + "\n" + root.Config + "\n" + root.ID)
		imgui.TreePop()
	}
	if root.Parent != "" {
		imgui.TextWrapped("Move this marker in its containing source. Editing a shared source affects its other uses.")
	}
}

func (p *Panel) previewStatus() {
	switch {
	case p.operation.err != nil:
		failure := p.operation.err
		message := "Failed: " + p.operation.target + ". Previous result remains displayed."
		if p.current == nil {
			message = "Failed: " + p.operation.target + ". No preview available."
		}
		if imgui.SmallButton("Retry") {
			p.retryPreview(false)
		}
		imgui.SameLine()
		if imgui.SmallButton("Dismiss") {
			p.dismissRequest()
		}
		if imgui.SmallButton("Details") {
			imgui.OpenPopup("preview-error")
		}
		imgui.TextWrapped(message)
		if imgui.BeginPopup("preview-error") {
			imgui.TextWrapped(failure.Error())
			var admission *resources.AdmissionError
			if errors.As(failure, &admission) {
				label := "Estimated allocation requested"
				if admission.Incremental {
					label = "Additional reservation requested"
				}
				imgui.TextWrapped(fmt.Sprintf("%s: %d bytes; remaining admission: %d bytes; host available: %d bytes", label, admission.Needed, admission.Available, admission.HostAvailable))
				for owner, bytes := range admission.Reservations {
					imgui.TextWrapped(fmt.Sprintf("%s: %d reserved bytes", owner, bytes))
				}
				imgui.TextWrapped("Reservations are estimates, not measured Go heap, process working set, or GPU allocations.")
			}
			if imgui.Button("Release optional preview and retry") {
				p.retryPreview(true)
				imgui.CloseCurrentPopup()
			}
			imgui.EndPopup()
		}
	case p.pending != nil || p.results != nil:
		message := "Updating " + p.operation.target + "... Previous result shown."
		if p.current == nil {
			message = "Building " + p.operation.target + "..."
		}
		imgui.TextWrapped(message)
	case p.stale:
		imgui.TextWrapped("Stale - refresh deferred until this view is needed.")
	case p.current == nil:
		imgui.TextWrapped("Show the preview to load this editor scenario.")
	case p.views[1] != nil && !p.views[1].Render().LevelReady(int(p.level)):
		if p.views[1].Render().GeometryWaiting() {
			imgui.TextWrapped("View waiting for geometry memory. Close an unused map or release the preview.")
			if imgui.SmallButton("Release preview and retry") {
				p.retryPreview(true)
			}
		} else {
			imgui.TextWrapped("Preparing view for this deck...")
		}
	default:
		imgui.TextWrapped("Ready · editor scenario")
	}
}
