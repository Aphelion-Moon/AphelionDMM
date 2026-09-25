package mappingui

import (
	"fmt"
	"github.com/SpaiR/imgui-go"
	"strings"
)

func (p *Panel) advisoryControls() {
	if p.current.advisory == nil || !imgui.TreeNode("Area-spawn authored guidance") {
		return
	}
	defer imgui.TreePop()
	spawns := p.current.advisory.Spawns
	if len(spawns) == 0 {
		imgui.TextWrapped("No resolved area-spawn entries in this project.")
		return
	}
	p.spawnSelection = max(0, min(p.spawnSelection, len(spawns)-1))
	if imgui.BeginCombo("Datum", spawns[p.spawnSelection].Rule.Path) {
		for i, spawn := range spawns {
			if imgui.Selectable(spawn.Rule.Path) {
				p.spawnSelection = i
				p.spawnPage = 0
				p.spawnTarget = 0
			}
		}
		imgui.EndCombo()
	}
	spawn := spawns[p.spawnSelection]
	rule := spawn.Rule
	imgui.TextWrapped("Desired type: " + rule.Desired)
	if rule.IsOver {
		imgui.TextWrapped("Separate over pass: one spawn per matching turf; no amount, mode or priority. Hosts: " + strings.Join(rule.Over, ", "))
	} else {
		imgui.TextWrapped(fmt.Sprintf("Requested amount %d; mode %d (0 open, 1 hug wall, 2 mount wall); optional %t", rule.Amount, rule.Mode, rule.Optional))
	}
	imgui.TextWrapped("Target priority: " + strings.Join(rule.Targets, " → "))
	imgui.TextWrapped("Station blacklist: " + strings.Join(rule.Blacklist, ", "))
	imgui.TextWrapped(spawn.Status)
	if spawn.SelectedTarget != "" {
		imgui.TextWrapped("First authored target with candidates: " + spawn.SelectedTarget)
	} else {
		imgui.TextWrapped("Target selection unresolved or has no known eligible authored cells.")
	}
	for i, target := range spawn.Targets {
		if imgui.Selectable(fmt.Sprintf("%s: %d eligible, %d rejected, %d unresolved", target.Path, target.Eligible, target.Rejected, target.Unknown)) {
			p.spawnTarget = i
			p.spawnPage = 0
		}
	}
	if len(spawn.Targets) == 0 {
		return
	}
	p.spawnTarget = max(0, min(p.spawnTarget, len(spawn.Targets)-1))
	cells := spawn.Targets[p.spawnTarget].Cells
	const pageSize = 100
	pages := max(1, (len(cells)+pageSize-1)/pageSize)
	p.spawnPage = max(0, min(p.spawnPage, pages-1))
	if imgui.SmallButton("Previous candidate page") {
		p.spawnPage = max(0, p.spawnPage-1)
	}
	imgui.SameLine()
	if imgui.SmallButton("Next candidate page") {
		p.spawnPage = min(pages-1, p.spawnPage+1)
	}
	imgui.Text(fmt.Sprintf("Page %d / %d (%d assessed cells)", p.spawnPage+1, pages, len(cells)))
	imgui.BeginChildV("spawn-cells", imgui.Vec2{Y: 180}, true, imgui.WindowFlagsNone)
	for i := p.spawnPage * pageSize; i < min(len(cells), (p.spawnPage+1)*pageSize); i++ {
		cell := cells[i]
		imgui.PushIDInt(i)
		if imgui.SmallButton("Inspect") {
			p.navigate(cell.Point)
		}
		imgui.SameLine()
		imgui.TextWrapped(fmt.Sprintf("%d,%d,%d: score %d; unresolved=%t — %s", cell.Point.X, cell.Point.Y, cell.Point.Z, cell.Score, cell.Unknown, cell.Reason))
		imgui.PopID()
	}
	imgui.EndChild()
}
