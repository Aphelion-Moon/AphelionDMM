package ui

import (
	"errors"
	"fmt"
	"strings"

	"sdmm/internal/aphelion/repath"
	"sdmm/internal/app/ui/component"

	"github.com/SpaiR/imgui-go"
	native "github.com/sqweek/dialog"
)

// Panel draws the Controller. It holds only transient widget state.
type Panel struct {
	component.Component

	Controller *Controller

	filter         string
	unresolvedOnly bool
	custom         map[string]string
	editing        string
	confirmDelete  string
	scriptName     string
	confirmReplace bool
	saveError      string
}

func NewPanel(app App) *Panel {
	return &Panel{Controller: NewController(app), custom: map[string]string{}}
}

// OfferFor brings a newly opened map with unknown types to the panel.
func (p *Panel) OfferFor(mapPath string) { p.Controller.OfferFor(mapPath) }

var tierColors = map[repath.Tier]imgui.Vec4{
	repath.Certain: {X: 0.45, Y: 0.85, Z: 0.45, W: 1},
	repath.High:    {X: 0.65, Y: 0.85, Z: 0.45, W: 1},
	repath.Medium:  {X: 0.95, Y: 0.8, Z: 0.35, W: 1},
	repath.Low:     {X: 0.95, Y: 0.55, Z: 0.35, W: 1},
	repath.Lossy:   {X: 0.9, Y: 0.4, Z: 0.4, W: 1},
}

func (p *Panel) Process(int32) {
	c := p.Controller
	c.Update()
	if c.app.LoadedEnvironment() == nil {
		imgui.TextDisabled("Open an environment to resolve unknown types.")
		return
	}
	overview := c.Overview()
	switch {
	case overview.IndexError != "":
		imgui.TextWrapped("Unable to index the environment: " + overview.IndexError)
		return
	case overview.Indexing:
		imgui.TextDisabled("Indexing environment types...")
		return
	case overview.MapName == "":
		imgui.TextDisabled("Activate a map to resolve its unknown types.")
		return
	}
	p.header(overview)
	p.sources(overview)
	imgui.Separator()
	if overview.Types == 0 {
		if overview.Analyzing {
			imgui.TextDisabled("Analyzing...")
		} else {
			imgui.TextDisabled("Every type on this map is defined by the environment.")
		}
		p.messages()
		return
	}
	p.table()
	imgui.Separator()
	p.footer()
	p.messages()
}

func (p *Panel) header(overview Overview) {
	imgui.Text(fmt.Sprintf("%s: %d unknown types, %d instances on %d tiles", overview.MapName, overview.Types, overview.Instances, overview.Tiles))
	if overview.Analyzing {
		imgui.SameLine()
		imgui.TextDisabled("(analyzing)")
	}
	if overview.AnalysisError != "" {
		imgui.TextWrapped("Analysis failed: " + overview.AnalysisError)
	}
	counts := p.Controller.StatusCounts()
	var parts []string
	for _, status := range []string{"Auto", "Chosen", "Unresolved", "No match", "Kept", "Delete"} {
		if counts[status] != 0 {
			parts = append(parts, fmt.Sprintf("%s %d", status, counts[status]))
		}
	}
	imgui.TextDisabled(strings.Join(parts, " | "))
}

func (p *Panel) sources(overview Overview) {
	c := p.Controller
	if !imgui.CollapsingHeaderV("Sources", imgui.TreeNodeFlagsNone) {
		return
	}
	imgui.Text(fmt.Sprintf("UpdatePaths: %d scripts, %d rules", overview.Scripts, overview.Rules))
	if len(overview.ScriptErrors) != 0 {
		imgui.SameLine()
		imgui.TextColored(tierColors[repath.Low], fmt.Sprintf("%d errors", len(overview.ScriptErrors)))
		if imgui.IsItemHovered() {
			var lines []string
			for n, err := range overview.ScriptErrors {
				if n == 20 {
					lines = append(lines, fmt.Sprintf("... %d more", len(overview.ScriptErrors)-n))
					break
				}
				lines = append(lines, err.Error())
			}
			imgui.SetTooltip(strings.Join(lines, "\n"))
		}
	}
	if imgui.Button("Reload Scripts") {
		c.ReloadScripts()
	}
	imgui.SameLine()
	if imgui.Button("Load Script File...") {
		if path, err := native.File().Title("Load UpdatePaths Script").Filter("UpdatePaths script", "txt").Load(); err == nil {
			c.AddScriptFile(path)
		} else if !errors.Is(err, native.ErrCancelled) {
			c.Error = err.Error()
		}
	}
	if files := c.ScriptFiles(); len(files) != 0 {
		imgui.SameLine()
		if imgui.Button(fmt.Sprintf("Clear %d Loaded", len(files))) {
			c.ClearScriptFiles()
		}
	}

	path, loading, progress, referenceErr := c.ReferenceState()
	switch {
	case loading:
		imgui.Text("Reference: loading " + path)
		imgui.TextDisabled(progress)
		if imgui.Button("Cancel Reference") {
			c.UnloadReference()
		}
	case path != "":
		imgui.TextWrapped("Reference: " + path)
		if imgui.Button("Unload Reference") {
			c.UnloadReference()
		}
	default:
		imgui.TextDisabled("Reference: none (load the source codebase's .dme to match by appearance)")
		if imgui.Button("Load Reference Environment...") {
			if chosen, err := native.File().Title("Source Environment").Filter("DreamMaker environment", "dme").Load(); err == nil {
				c.LoadReference(chosen)
			} else if !errors.Is(err, native.ErrCancelled) {
				c.Error = err.Error()
			}
		}
	}
	if referenceErr != "" {
		imgui.TextWrapped("Reference failed: " + referenceErr)
	}
	if count := c.RememberedCount(); count != 0 {
		imgui.Text(fmt.Sprintf("Remembered decisions: %d", count))
		imgui.SameLine()
		if imgui.Button("Forget") {
			c.ForgetRemembered()
		}
	}
}

func (p *Panel) table() {
	c := p.Controller
	imgui.SetNextItemWidth(-200)
	imgui.InputTextWithHint("##path-migration-filter", "Filter paths", &p.filter)
	imgui.SameLine()
	imgui.Checkbox("Unresolved only", &p.unresolvedOnly)
	var rows []Row
	for _, row := range c.Rows() {
		if p.filter != "" && !strings.Contains(row.Proposal.Entry.Path, p.filter) {
			continue
		}
		if p.unresolvedOnly && row.Choice.Kind != ChoiceNone {
			continue
		}
		rows = append(rows, row)
	}
	height := -imgui.FrameHeightWithSpacing() * 4
	flags := imgui.TableFlagsBordersInner | imgui.TableFlagsResizable | imgui.TableFlagsScrollY | imgui.TableFlagsRowBg | imgui.TableFlagsNoSavedSettings
	if !imgui.BeginTableV("path-migration", 5, flags, imgui.Vec2{Y: height}, 0) {
		return
	}
	imgui.TableSetupScrollFreeze(0, 1)
	imgui.TableSetupColumnV("Status", imgui.TableColumnFlagsWidthFixed, 70, 0)
	imgui.TableSetupColumnV("Unknown path", imgui.TableColumnFlagsWidthStretch, 1, 0)
	imgui.TableSetupColumnV("Count", imgui.TableColumnFlagsWidthFixed, 50, 0)
	imgui.TableSetupColumnV("Resolve to", imgui.TableColumnFlagsWidthStretch, 1.2, 0)
	imgui.TableSetupColumnV("", imgui.TableColumnFlagsWidthFixed, 40, 0)
	imgui.TableHeadersRow()
	for _, row := range rows {
		p.row(row)
	}
	imgui.EndTable()
}

func candidateLabel(candidate repath.Candidate) string {
	target := candidate.Target
	switch {
	case candidate.Decision.Kind == repath.Delete:
		target = "Delete"
	case target == "":
		target = candidate.Decision.Summary
	}
	return fmt.Sprintf("[%s] %s", candidate.Tier, target)
}

func (p *Panel) row(row Row) {
	c := p.Controller
	entry := row.Proposal.Entry
	imgui.PushID(entry.Path)
	defer imgui.PopID()
	imgui.TableNextRow()

	imgui.TableNextColumn()
	imgui.AlignTextToFramePadding()
	imgui.Text(StatusOf(row))

	imgui.TableNextColumn()
	imgui.AlignTextToFramePadding()
	imgui.Text(entry.Path)
	if imgui.IsItemHovered() {
		imgui.SetTooltip(variantsTooltip(entry))
	}

	imgui.TableNextColumn()
	imgui.AlignTextToFramePadding()
	imgui.Text(fmt.Sprint(entry.Count))

	imgui.TableNextColumn()
	p.choiceCombo(row)

	imgui.TableNextColumn()
	if imgui.Button("Find") {
		c.Find(entry.Path)
	}
}

func variantsTooltip(entry repath.Entry) string {
	lines := []string{fmt.Sprintf("%d instances on %d tiles, %d variable variants", entry.Count, entry.Tiles, len(entry.Variants))}
	for n, variant := range entry.Variants {
		if n == 8 {
			lines = append(lines, fmt.Sprintf("... %d more variants", len(entry.Variants)-n))
			break
		}
		edits := strings.ReplaceAll(strings.TrimSuffix(variant.Key, "\n"), "\n", "; ")
		if edits == "" {
			edits = "(no map edits)"
		}
		lines = append(lines, fmt.Sprintf("%dx %s", variant.Count, edits))
	}
	if entry.Overflow != 0 {
		lines = append(lines, fmt.Sprintf("%d instances with further variants", entry.Overflow))
	}
	return strings.Join(lines, "\n")
}

func candidateTooltip(candidate repath.Candidate) string {
	lines := []string{fmt.Sprintf("%s confidence (%.2f) from %s", candidate.Tier, candidate.Score, candidate.Generator)}
	lines = append(lines, candidate.Reasons...)
	if len(candidate.MissingVars) != 0 {
		lines = append(lines, "Not declared by the target: "+strings.Join(candidate.MissingVars, ", "))
	}
	if len(candidate.Conflicts) != 0 {
		lines = append(lines, "Overwrites map edits: "+strings.Join(candidate.Conflicts, "; "))
	}
	if candidate.Decision.Via == repath.ViaRule && candidate.Decision.Kind == repath.Apply {
		lines = append(lines, "Rule: "+candidate.Decision.Rule.String())
	}
	return strings.Join(lines, "\n")
}

func (p *Panel) choiceCombo(row Row) {
	c := p.Controller
	path := row.Proposal.Entry.Path
	preview := "Choose..."
	var current *repath.Candidate
	switch row.Choice.Kind {
	case ChoiceCandidate:
		current = &row.Proposal.Candidates[row.Choice.Candidate]
		preview = candidateLabel(*current)
	case ChoiceCustom:
		preview = "Custom: " + row.Choice.Custom
	case ChoiceKeep:
		preview = "Keep unknown"
	case ChoiceDelete:
		preview = "Delete"
	default:
		if len(row.Proposal.Candidates) == 0 {
			preview = "No suggestion"
		}
	}
	if current != nil {
		imgui.PushStyleColor(imgui.StyleColorText, tierColors[current.Tier])
	}
	imgui.SetNextItemWidth(-1)
	open := imgui.BeginComboV("##choice", preview, imgui.ComboFlagsHeightLarge)
	if current != nil {
		imgui.PopStyleColor()
		if !open && imgui.IsItemHovered() {
			imgui.SetTooltip(candidateTooltip(*current))
		}
	}
	if open {
		for n, candidate := range row.Proposal.Candidates {
			imgui.PushStyleColor(imgui.StyleColorText, tierColors[candidate.Tier])
			selected := row.Choice.Kind == ChoiceCandidate && row.Choice.Candidate == n
			if imgui.SelectableV(fmt.Sprintf("%s##%d", candidateLabel(candidate), n), selected, imgui.SelectableFlagsNone, imgui.Vec2{}) {
				if candidate.Decision.Kind == repath.Delete {
					p.confirmDelete = path
				} else {
					c.Choose(path, Choice{Kind: ChoiceCandidate, Candidate: n, AllowCrossBase: candidate.CrossBase && row.Choice.AllowCrossBase})
				}
			}
			imgui.PopStyleColor()
			if imgui.IsItemHovered() {
				imgui.SetTooltip(candidateTooltip(candidate))
			}
		}
		imgui.Separator()
		if imgui.SelectableV("Custom path...", row.Choice.Kind == ChoiceCustom, imgui.SelectableFlagsNone, imgui.Vec2{}) {
			p.editing = path
			if _, ok := p.custom[path]; !ok {
				p.custom[path] = repath.Parent(path)
			}
		}
		if imgui.SelectableV("Keep unknown", row.Choice.Kind == ChoiceKeep, imgui.SelectableFlagsNone, imgui.Vec2{}) {
			c.Choose(path, Choice{Kind: ChoiceKeep})
		}
		if imgui.SelectableV("Delete instances...", row.Choice.Kind == ChoiceDelete, imgui.SelectableFlagsNone, imgui.Vec2{}) {
			p.confirmDelete = path
		}
		imgui.EndCombo()
	}
	if current != nil && current.CrossBase {
		allow := row.Choice.AllowCrossBase
		if imgui.Checkbox("Allow root type change", &allow) {
			choice := row.Choice
			choice.AllowCrossBase = allow
			c.Choose(path, choice)
		}
	}
	if p.editing == path {
		p.customEditor(row)
	}
	if p.confirmDelete == path {
		imgui.TextColored(tierColors[repath.Lossy], fmt.Sprintf("Delete all %d instances?", row.Proposal.Entry.Count))
		imgui.SameLine()
		if imgui.Button("Delete") {
			c.Choose(path, Choice{Kind: ChoiceDelete})
			p.confirmDelete = ""
		}
		imgui.SameLine()
		if imgui.Button("Cancel") {
			p.confirmDelete = ""
		}
	}
}

func (p *Panel) customEditor(row Row) {
	c := p.Controller
	path := row.Proposal.Entry.Path
	text := p.custom[path]
	imgui.SetNextItemWidth(-1)
	if imgui.InputTextWithHint("##custom", "/obj/...", &text) {
		p.custom[path] = text
	}
	known := c.Known(text)
	crossBase := known && !repath.SameBase(path, text)
	allow := row.Choice.Kind == ChoiceCustom && row.Choice.AllowCrossBase
	if crossBase {
		imgui.Checkbox("Allow root type change##custom", &allow)
	}
	if !known {
		for _, match := range c.CustomMatches(text) {
			if imgui.SelectableV(match, false, imgui.SelectableFlagsNone, imgui.Vec2{}) {
				p.custom[path] = match
			}
		}
	}
	imgui.BeginDisabledV(!known || crossBase && !allow)
	if imgui.Button("Use Path") {
		c.Choose(path, Choice{Kind: ChoiceCustom, Custom: text, AllowCrossBase: allow})
		p.editing = ""
	}
	imgui.EndDisabled()
	imgui.SameLine()
	if imgui.Button("Close") {
		p.editing = ""
	}
	if text != "" && !known {
		imgui.SameLine()
		imgui.TextDisabled("not defined")
	}
}

func (p *Panel) footer() {
	c := p.Controller
	if imgui.Button("Accept All High") {
		c.AcceptAtLeast(repath.High)
	}
	imgui.SameLine()
	paths, instances := c.Pending()
	imgui.BeginDisabledV(!c.CanApply())
	if imgui.Button(fmt.Sprintf("Apply Migration (%d types, %d instances)", paths, instances)) {
		c.Apply()
	}
	imgui.EndDisabled()
	settings := c.settings()
	if settings.RememberDecisions {
		imgui.SameLine()
		imgui.Checkbox("Remember", &c.Remember)
		if imgui.IsItemHovered() {
			imgui.SetTooltip("Keep these rule choices for maps of this environment.")
		}
	}
	if imgui.Button("Export Script...") {
		path, err := native.File().Title("Export UpdatePaths Script").Filter("UpdatePaths script", "txt").SetStartFile("path_migration.txt").Save()
		switch {
		case err == nil:
			if err := WriteFile(path, c.ExportScript()); err != nil {
				c.Error = err.Error()
			} else {
				c.Status = "Exported " + path
			}
		case !errors.Is(err, native.ErrCancelled):
			c.Error = err.Error()
		}
	}
	if settings.WriteCodebaseScripts {
		imgui.SameLine()
		imgui.SetNextItemWidth(220)
		if imgui.InputTextWithHint("##script-name", "12345_DESCRIPTIVE_NAME.txt", &p.scriptName) {
			p.confirmReplace, p.saveError = false, ""
		}
		imgui.SameLine()
		label := "Save to Codebase"
		if p.confirmReplace {
			label = "Replace Existing Script"
		}
		if imgui.Button(label) {
			saved, err := c.SaveCodebaseScript(p.scriptName, p.confirmReplace)
			switch {
			case errors.Is(err, ErrScriptExists):
				p.confirmReplace, p.saveError = true, err.Error()
			case err != nil:
				p.confirmReplace, p.saveError = false, err.Error()
			default:
				p.confirmReplace, p.saveError = false, ""
				c.Status = "Saved " + saved
			}
		}
		if p.saveError != "" {
			imgui.TextWrapped(p.saveError)
		}
	}
}

func (p *Panel) messages() {
	c := p.Controller
	if c.Status != "" {
		imgui.TextWrapped(c.Status)
	}
	if c.Error != "" {
		imgui.PushStyleColor(imgui.StyleColorText, tierColors[repath.Lossy])
		imgui.TextWrapped(c.Error)
		imgui.PopStyleColor()
	}
}

// Background keeps a pending automatic application moving while the panel is
// closed or hidden behind another tab. Call once per frame.
func (p *Panel) Background(visible bool) {
	if !visible && p.Controller.autoApplyAfter {
		p.Controller.Update()
	}
}
