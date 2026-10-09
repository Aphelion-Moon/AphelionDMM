// Package ui presents path migration for the active map: it keeps the
// analysis current, collects a person's decisions and applies them through the
// map's editor. Controller holds all state and work; Panel only draws it.
package ui

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"

	"sdmm/internal/aphelion/envsnapshot"
	"sdmm/internal/aphelion/repath"
	"sdmm/internal/aphelion/repath/envtypes"
	"sdmm/internal/aphelion/repath/updatepaths"
	"sdmm/internal/dmapi/dmenv"
)

// Target is the active map as seen by path migration. All methods run on the
// UI thread; the function returned by CaptureInventory runs on a worker.
type Target interface {
	ID() string
	Name() string
	Path() string // absolute map file path
	Version() (generation uint64, revision uint64)
	CanEdit() bool
	CaptureInventory(known func(string) bool) (func(context.Context) (repath.Inventory, error), error)
	Apply(t *repath.Transformer, label string, done func(repath.Report, error)) error
}

type App interface {
	LoadedEnvironment() *dmenv.Dme
	PathMigrationTarget() (Target, bool)
	PathMigrationSettings() *repath.Settings
	PathMigrationMemory() *repath.Memory
	SavePathMigrationMemory()
	ShowPathInSearch(string)
	RunLater(func())
	BypassEnvironmentCache() bool
}

// ParseEnvironment loads a reference environment; tests replace it.
var ParseEnvironment = func(ctx context.Context, path string, bypassCache bool, progress func(string)) (*dmenv.Dme, error) {
	return dmenv.NewWithProgress(ctx, path, envsnapshot.Options{Enabled: !bypassCache}, progress)
}

type ChoiceKind uint8

const (
	ChoiceNone ChoiceKind = iota
	ChoiceCandidate
	ChoiceCustom
	ChoiceKeep
	ChoiceDelete
)

// Choice is the selected outcome for one unknown path.
type Choice struct {
	Kind           ChoiceKind
	Candidate      int
	candidateKey   string
	Custom         string
	AllowCrossBase bool
	Human          bool
}

type Row struct {
	Proposal repath.Proposal
	Choice   Choice
}

const (
	scriptsDir       = "tools/UpdatePaths/Scripts"
	maxScriptBytes   = 4 << 20
	maxScriptFiles   = 4096
	migrationLabel   = "Migrate Unknown Types"
	maxCustomMatches = 20
)

type analysisKey struct {
	target               string
	generation, revision uint64
	env                  *dmenv.Dme
	reference, scripts   uint64
	remembered           uint64
	auto                 repath.AutoLevel
}

type Controller struct {
	app App

	env        *dmenv.Dme
	index      *repath.Index
	indexing   bool
	indexError string

	scripts       *updatepaths.Set
	scriptErrors  []updatepaths.ParseError
	scriptFiles   []string // person-loaded script files
	scriptsLoaded bool
	scriptsGen    uint64
	scriptsBusy   bool

	referencePath     string
	reference         *repath.Index
	referenceGen      uint64
	referenceCancel   context.CancelFunc
	referenceError    string
	progressMu        sync.Mutex
	referenceProgress string

	remembered    []updatepaths.Rule
	rememberedGen uint64

	installed analysisKey
	// choices keeps a person's choices per target and path across analyses, so
	// rows that an applied migration removed regain them after undo.
	choices     map[string]map[string]Choice
	running     *analysisKey
	cancel      context.CancelFunc
	target      Target
	targetName  string
	inventory   repath.Inventory
	rows        []Row
	analysisErr string

	applying       bool
	Remember       bool
	Status         string
	Error          string
	autoApplyFor   string // map name offered on open with ApplyCertainOnOpen
	autoApplyAfter bool
}

func NewController(app App) *Controller {
	return &Controller{app: app}
}

func (c *Controller) settings() repath.Settings {
	if settings := c.app.PathMigrationSettings(); settings != nil {
		return *settings
	}
	return repath.DefaultSettings()
}

// OfferFor marks a newly opened map by absolute path. With ApplyCertainOnOpen, its Certain
// selections are applied once its first analysis completes.
func (c *Controller) OfferFor(mapPath string) {
	c.autoApplyFor = mapPath
	c.autoApplyAfter = c.settings().ApplyCertainOnOpen
}

// Update keeps sources and analysis current. Call once per frame.
func (c *Controller) Update() {
	env := c.app.LoadedEnvironment()
	if env != c.env {
		c.resetEnvironment(env)
	}
	if env == nil {
		return
	}
	if c.index == nil && !c.indexing && c.indexError == "" {
		c.startIndex(env)
	}
	if !c.scriptsLoaded && !c.scriptsBusy {
		c.startScripts()
	}
	target, ok := c.app.PathMigrationTarget()
	if !ok {
		c.target, c.targetName = nil, ""
		c.rows, c.inventory = nil, repath.Inventory{}
		return
	}
	c.target, c.targetName = target, target.Name()
	// Scripts load quickly and decide Certain results; wait for them so a
	// first analysis never offers a weaker answer than the codebase gives.
	if c.index == nil || c.applying || !c.scriptsLoaded {
		return
	}
	generation, revision := target.Version()
	key := analysisKey{target: target.ID(), generation: generation, revision: revision, env: env, reference: c.referenceGen, scripts: c.scriptsGen, remembered: c.rememberedGen, auto: c.settings().Level()}
	if key == c.installed || c.running != nil && *c.running == key {
		return
	}
	c.startAnalysis(target, key)
}

func (c *Controller) resetEnvironment(env *dmenv.Dme) {
	if c.cancel != nil {
		c.cancel()
	}
	if c.referenceCancel != nil {
		c.referenceCancel()
	}
	c.env, c.index, c.indexing, c.indexError = env, nil, false, ""
	c.scripts, c.scriptErrors, c.scriptsLoaded, c.scriptsBusy = nil, nil, false, false
	c.scriptsGen++
	c.referencePath, c.reference, c.referenceCancel, c.referenceError = "", nil, nil, ""
	c.referenceGen++
	c.setProgress("")
	c.installed, c.running, c.cancel, c.choices = analysisKey{}, nil, nil, nil
	c.inventory, c.rows, c.analysisErr = repath.Inventory{}, nil, ""
	c.applying, c.Status, c.Error = false, "", ""
	if env == nil {
		return
	}
	c.loadRemembered()
	if memory := c.app.PathMigrationMemory(); memory != nil && c.settings().RememberReferencePath {
		if path := memory.Environments[repath.EnvironmentKey(env.RootFile)].Reference; path != "" {
			c.LoadReference(path)
		}
	}
}

func (c *Controller) loadRemembered() {
	c.remembered = nil
	c.rememberedGen++
	memory := c.app.PathMigrationMemory()
	if memory == nil || c.env == nil {
		return
	}
	rules, errs := memory.Rules(repath.EnvironmentKey(c.env.RootFile))
	c.remembered = rules
	if len(errs) != 0 {
		c.Error = errors.Join(errs...).Error()
	}
}

func (c *Controller) startIndex(env *dmenv.Dme) {
	c.indexing = true
	go func() {
		index, err := repath.NewIndex(context.Background(), envtypes.New(env))
		c.app.RunLater(func() {
			if c.env != env {
				return
			}
			c.indexing = false
			if err != nil {
				c.indexError = err.Error()
				return
			}
			c.index = index
		})
	}()
}

// ReloadScripts rereads codebase and person-loaded scripts.
func (c *Controller) ReloadScripts() {
	c.scriptsLoaded = false
}

// AddScriptFile includes a person-chosen script with the codebase scripts.
func (c *Controller) AddScriptFile(path string) {
	if !slices.Contains(c.scriptFiles, path) {
		c.scriptFiles = append(c.scriptFiles, path)
	}
	c.ReloadScripts()
}

func (c *Controller) ClearScriptFiles() {
	c.scriptFiles = nil
	c.ReloadScripts()
}

func (c *Controller) ScriptFiles() []string { return c.scriptFiles }

func (c *Controller) startScripts() {
	c.scriptsBusy = true
	env, files := c.env, slices.Clone(c.scriptFiles)
	readCodebase := c.settings().ReadCodebaseScripts
	go func() {
		set, errs := readScripts(env.RootDir, readCodebase, files)
		c.app.RunLater(func() {
			if c.env != env {
				return
			}
			c.scriptsBusy, c.scriptsLoaded = false, true
			c.scripts, c.scriptErrors = set, errs
			c.scriptsGen++
		})
	}()
}

func readScripts(root string, readCodebase bool, files []string) (*updatepaths.Set, []updatepaths.ParseError) {
	var scripts []updatepaths.Script
	var errs []updatepaths.ParseError
	read := func(path, name string) {
		info, err := os.Stat(path)
		if err == nil && info.Size() > maxScriptBytes {
			err = fmt.Errorf("larger than %d bytes", maxScriptBytes)
		}
		var data []byte
		if err == nil {
			data, err = os.ReadFile(path)
		}
		if err != nil {
			errs = append(errs, updatepaths.ParseError{Pos: updatepaths.Pos{File: name}, Msg: err.Error()})
			return
		}
		script, parseErrs := updatepaths.Parse(name, data)
		scripts = append(scripts, script)
		errs = append(errs, parseErrs...)
	}
	if readCodebase && root != "" {
		dir := filepath.Join(root, filepath.FromSlash(scriptsDir))
		entries, err := os.ReadDir(dir)
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			errs = append(errs, updatepaths.ParseError{Pos: updatepaths.Pos{File: scriptsDir}, Msg: err.Error()})
		}
		count := 0
		for _, entry := range entries {
			if entry.IsDir() || !strings.EqualFold(filepath.Ext(entry.Name()), ".txt") {
				continue
			}
			if count++; count > maxScriptFiles {
				errs = append(errs, updatepaths.ParseError{Pos: updatepaths.Pos{File: scriptsDir}, Msg: fmt.Sprintf("more than %d scripts; the rest were skipped", maxScriptFiles)})
				break
			}
			read(filepath.Join(dir, entry.Name()), entry.Name())
		}
	}
	for _, file := range files {
		read(file, filepath.Base(file))
	}
	return updatepaths.NewSet(scripts...), errs
}

// LoadReference parses another codebase's environment to compare types.
func (c *Controller) LoadReference(path string) {
	if c.env == nil {
		return
	}
	if filepath.Clean(path) == filepath.Clean(c.env.RootFile) {
		c.referenceError = "the reference must be a different environment from the loaded one"
		return
	}
	if c.referenceCancel != nil {
		c.referenceCancel()
	}
	ctx, cancel := context.WithCancel(context.Background())
	c.referenceCancel, c.referencePath, c.referenceError, c.reference = cancel, path, "", nil
	c.referenceGen++
	generation, env := c.referenceGen, c.env
	bypass := c.app.BypassEnvironmentCache()
	c.setProgress("Starting")
	go func() {
		reference, err := ParseEnvironment(ctx, path, bypass, c.setProgress)
		var index *repath.Index
		if err == nil {
			c.setProgress("Indexing types")
			index, err = repath.NewIndex(ctx, envtypes.New(reference))
		}
		c.app.RunLater(func() {
			if c.referenceGen != generation || c.env != env {
				return
			}
			c.referenceCancel = nil
			c.setProgress("")
			if err != nil {
				if !errors.Is(err, context.Canceled) {
					c.referenceError = err.Error()
				}
				c.referencePath = ""
				c.referenceGen++
				return
			}
			c.reference = index
			c.referenceGen++
			if c.settings().RememberReferencePath {
				c.rememberReference(path)
			}
		})
	}()
}

func (c *Controller) rememberReference(path string) {
	memory := c.app.PathMigrationMemory()
	if memory == nil || c.env == nil {
		return
	}
	key := repath.EnvironmentKey(c.env.RootFile)
	environment := memory.Environments[key]
	environment.Reference = path
	memory.Environments[key] = environment
	c.app.SavePathMigrationMemory()
}

func (c *Controller) UnloadReference() {
	if c.referenceCancel != nil {
		c.referenceCancel()
		c.referenceCancel = nil
	}
	c.reference, c.referencePath, c.referenceError = nil, "", ""
	c.setProgress("")
	c.referenceGen++
}

func (c *Controller) setProgress(stage string) {
	c.progressMu.Lock()
	c.referenceProgress = stage
	c.progressMu.Unlock()
}

// ReferenceState returns the reference path, whether it is loading, its
// progress stage and the last error.
func (c *Controller) ReferenceState() (path string, loading bool, progress string, err string) {
	c.progressMu.Lock()
	progress = c.referenceProgress
	c.progressMu.Unlock()
	return c.referencePath, c.referenceCancel != nil, progress, c.referenceError
}

func (c *Controller) startAnalysis(target Target, key analysisKey) {
	if c.cancel != nil {
		c.cancel()
	}
	run, err := target.CaptureInventory(c.index.Exists)
	if err != nil {
		// The map is mid-edit; retry on a later frame.
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	c.cancel, c.running = cancel, &key
	sources := repath.Sources{Target: c.index, Reference: c.reference, Scripts: c.scripts, Remembered: c.remembered}
	go func() {
		inventory, err := run(ctx)
		var proposals []repath.Proposal
		if err == nil {
			proposals, err = repath.Suggest(ctx, inventory, sources, key.auto)
		}
		c.app.RunLater(func() {
			if c.running == nil || *c.running != key {
				return
			}
			c.running, c.cancel = nil, nil
			if err != nil {
				if !errors.Is(err, context.Canceled) {
					c.analysisErr = err.Error()
				}
				return
			}
			c.analysisErr = ""
			c.installed = key
			c.install(inventory, proposals)
			if c.autoApplyAfter && c.autoApplyFor == target.Path() {
				c.autoApplyAfter = false
				c.applyCertain()
			}
		})
	}()
}

// install replaces the analysis, keeping a person's choices that still apply.
func (c *Controller) install(inventory repath.Inventory, proposals []repath.Proposal) {
	previous := c.choices[c.installed.target]
	c.inventory = inventory
	c.rows = make([]Row, len(proposals))
	for n, proposal := range proposals {
		row := Row{Proposal: proposal}
		if proposal.Auto >= 0 {
			row.Choice = Choice{Kind: ChoiceCandidate, Candidate: proposal.Auto, candidateKey: candidateKey(proposal.Candidates[proposal.Auto])}
		}
		if choice, ok := previous[proposal.Entry.Path]; ok {
			if choice.Kind != ChoiceCandidate {
				row.Choice = choice
			} else if index := slices.IndexFunc(proposal.Candidates, func(candidate repath.Candidate) bool { return candidateKey(candidate) == choice.candidateKey }); index >= 0 {
				choice.Candidate = index
				row.Choice = choice
			}
		}
		c.rows[n] = row
	}
}

func candidateKey(candidate repath.Candidate) string {
	return fmt.Sprintf("%d/%d/%s", candidate.Decision.Kind, candidate.Decision.Via, candidate.Decision.Rule.String())
}

func (c *Controller) Rows() []Row { return c.rows }

func (c *Controller) Choose(path string, choice Choice) {
	for n := range c.rows {
		if c.rows[n].Proposal.Entry.Path != path {
			continue
		}
		choice.Human = true
		if choice.Kind == ChoiceCandidate {
			if choice.Candidate < 0 || choice.Candidate >= len(c.rows[n].Proposal.Candidates) {
				return
			}
			candidate := c.rows[n].Proposal.Candidates[choice.Candidate]
			choice.candidateKey = candidateKey(candidate)
			if candidate.Decision.Kind == repath.Delete {
				choice.Kind = ChoiceDelete
			}
		}
		c.rows[n].Choice = choice
		c.remember(path, choice)
		return
	}
}

func (c *Controller) remember(path string, choice Choice) {
	if c.choices == nil {
		c.choices = map[string]map[string]Choice{}
	}
	if c.choices[c.installed.target] == nil {
		c.choices[c.installed.target] = map[string]Choice{}
	}
	c.choices[c.installed.target][path] = choice
}

// AcceptAtLeast selects the best candidate of every unresolved row at or
// above tier, excluding deletions and root type changes.
func (c *Controller) AcceptAtLeast(tier repath.Tier) int {
	accepted := 0
	for n, row := range c.rows {
		if row.Choice.Kind != ChoiceNone || len(row.Proposal.Candidates) == 0 {
			continue
		}
		best := row.Proposal.Candidates[0]
		if best.Tier < tier || best.Decision.Kind != repath.Apply || best.CrossBase || best.Resolved == 0 {
			continue
		}
		c.rows[n].Choice = Choice{Kind: ChoiceCandidate, Candidate: 0, candidateKey: candidateKey(best), Human: true}
		c.remember(row.Proposal.Entry.Path, c.rows[n].Choice)
		accepted++
	}
	return accepted
}

// Plan builds the decisions of the rows accepted by filter, or of every row.
func (c *Controller) Plan(filter func(Row) bool) repath.Plan {
	plan := repath.NewPlan()
	for _, row := range c.rows {
		if filter != nil && !filter(row) {
			continue
		}
		path := row.Proposal.Entry.Path
		origin := repath.OriginAuto
		if row.Choice.Human {
			origin = repath.OriginHuman
		}
		switch row.Choice.Kind {
		case ChoiceCandidate:
			decision := row.Proposal.Candidates[row.Choice.Candidate].Decision
			decision.Origin, decision.AllowCrossBase = origin, row.Choice.AllowCrossBase
			plan.Paths[path] = decision
		case ChoiceCustom:
			plan.Paths[path] = repath.Decision{Kind: repath.Apply, Via: repath.ViaRule, Rule: repath.RepathRule(path, row.Choice.Custom, nil), Origin: origin, AllowCrossBase: row.Choice.AllowCrossBase, Summary: row.Choice.Custom}
		case ChoiceKeep:
			plan.Paths[path] = repath.Decision{Kind: repath.Keep, Origin: origin}
		case ChoiceDelete:
			if row.Choice.Human {
				plan.Paths[path] = repath.Decision{Kind: repath.Delete, Origin: repath.OriginHuman, Summary: "delete"}
			}
		}
	}
	return plan
}

func (c *Controller) resolvers() repath.Resolvers {
	return repath.Resolvers{Scripts: c.scripts, Remembered: c.remembered}
}

// Validate compiles the current choices without applying them.
func (c *Controller) Validate() error {
	_, err := c.compile(c.Plan(nil))
	return err
}

func (c *Controller) compile(plan repath.Plan) (*repath.Transformer, error) {
	if c.index == nil {
		return nil, errors.New("the environment is still being indexed")
	}
	return repath.Compile(plan, c.index, c.inventory.Paths(), c.resolvers())
}

// Pending counts rows whose choice would change the map.
func (c *Controller) Pending() (paths, instances int) {
	for _, row := range c.rows {
		if row.Choice.Kind == ChoiceCandidate || row.Choice.Kind == ChoiceCustom || row.Choice.Kind == ChoiceDelete && row.Choice.Human {
			paths++
			instances += row.Proposal.Entry.Count
		}
	}
	return paths, instances
}

func (c *Controller) Busy() bool {
	return c.applying || c.running != nil || c.indexing
}

func (c *Controller) CanApply() bool {
	paths, _ := c.Pending()
	return paths != 0 && !c.Busy() && c.target != nil && c.target.CanEdit()
}

// Apply submits every chosen migration as one edit.
func (c *Controller) Apply() {
	c.apply(c.Plan(nil), migrationLabel, c.Remember && c.settings().RememberDecisions, "")
}

func (c *Controller) applyCertain() {
	plan := c.Plan(func(row Row) bool {
		return !row.Choice.Human && row.Choice.Kind == ChoiceCandidate && row.Proposal.Candidates[row.Choice.Candidate].Tier == repath.Certain
	})
	if !plan.Active() {
		return
	}
	c.apply(plan, migrationLabel+" (Certain)", false, "Applied Certain migrations automatically; undo reverts them as one edit. ")
}

func (c *Controller) apply(plan repath.Plan, label string, remember bool, prefix string) {
	c.Status, c.Error = "", ""
	if c.target == nil || !c.target.CanEdit() {
		c.Error = "finish or cancel the current map edit first"
		return
	}
	transformer, err := c.compile(plan)
	if err != nil {
		c.Error = err.Error()
		return
	}
	target := c.target
	c.applying = true
	err = target.Apply(transformer, label, func(report repath.Report, err error) {
		c.applying = false
		if err != nil {
			c.Error = err.Error()
			return
		}
		c.Status = prefix + summarize(report)
		if remember {
			c.rememberPlan(plan)
		}
	})
	if err != nil {
		c.applying = false
		c.Error = err.Error()
	}
}

func summarize(report repath.Report) string {
	status := fmt.Sprintf("Migrated %d instances on %d tiles.", report.Instances(), report.Tiles)
	unresolved := 0
	for _, count := range report.Unresolved {
		unresolved += count
	}
	if unresolved != 0 {
		status += fmt.Sprintf(" %d instances could not reach a defined type and were left unchanged.", unresolved)
	}
	return status
}

func (c *Controller) rememberPlan(plan repath.Plan) {
	memory := c.app.PathMigrationMemory()
	if memory == nil || c.env == nil {
		return
	}
	if added := memory.Remember(repath.EnvironmentKey(c.env.RootFile), plan); added != 0 {
		c.app.SavePathMigrationMemory()
		c.Status += fmt.Sprintf(" Remembered %d decisions.", added)
		c.loadRemembered()
	}
}

// RememberedCount reports how many rules are remembered for this environment.
func (c *Controller) RememberedCount() int { return len(c.remembered) }

func (c *Controller) ForgetRemembered() {
	memory := c.app.PathMigrationMemory()
	if memory == nil || c.env == nil {
		return
	}
	memory.Forget(repath.EnvironmentKey(c.env.RootFile))
	c.app.SavePathMigrationMemory()
	c.loadRemembered()
}

// ExportScript writes the current choices as an UpdatePaths script.
func (c *Controller) ExportScript() []byte {
	return repath.ExportScript(c.Plan(nil), "Map: "+c.targetName)
}

var ErrScriptExists = errors.New("a script with this name already exists")

// CodebaseScriptsDir returns the codebase's scripts directory, confined to the
// environment root, or an error when it is unavailable.
func (c *Controller) CodebaseScriptsDir() (string, error) {
	if c.env == nil || c.env.RootDir == "" {
		return "", errors.New("no environment is loaded")
	}
	root, err := filepath.EvalSymlinks(c.env.RootDir)
	if err != nil {
		return "", err
	}
	dir, err := filepath.EvalSymlinks(filepath.Join(c.env.RootDir, filepath.FromSlash(scriptsDir)))
	if err != nil {
		return "", fmt.Errorf("this codebase has no %s directory", scriptsDir)
	}
	if relative, err := filepath.Rel(root, dir); err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) || filepath.IsAbs(relative) {
		return "", fmt.Errorf("%s resolves outside the codebase", scriptsDir)
	}
	return dir, nil
}

// SaveCodebaseScript writes the export into the codebase's scripts directory.
// It requires the WriteCodebaseScripts setting and a PRNUMBER_NAME.txt name.
func (c *Controller) SaveCodebaseScript(name string, overwrite bool) (string, error) {
	if !c.settings().WriteCodebaseScripts {
		return "", errors.New("saving into the codebase is disabled in preferences")
	}
	if !repath.ValidScriptName(name) {
		return "", errors.New("use a name like 12345_DESCRIPTIVE_NAME.txt")
	}
	dir, err := c.CodebaseScriptsDir()
	if err != nil {
		return "", err
	}
	path := filepath.Join(dir, name)
	if info, err := os.Lstat(path); err == nil {
		if !info.Mode().IsRegular() {
			return "", fmt.Errorf("%s is not a regular file", name)
		}
		if !overwrite {
			return "", ErrScriptExists
		}
	}
	if err := writeAtomic(path, c.ExportScript()); err != nil {
		return "", err
	}
	c.ReloadScripts()
	return path, nil
}

// WriteFile writes an export to a person-chosen path.
func WriteFile(path string, data []byte) error { return writeAtomic(path, data) }

func writeAtomic(path string, data []byte) error {
	file, err := os.CreateTemp(filepath.Dir(path), ".updatepaths-*")
	if err != nil {
		return err
	}
	defer func() { _ = os.Remove(file.Name()) }()
	if _, err = file.Write(data); err == nil {
		err = file.Sync()
	}
	if closeErr := file.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	return os.Rename(file.Name(), path)
}

// CustomMatches suggests defined paths for a typed prefix.
func (c *Controller) CustomMatches(prefix string) []string {
	if c.index == nil || !strings.HasPrefix(prefix, "/") {
		return nil
	}
	return c.index.WithPrefix(prefix, maxCustomMatches)
}

func (c *Controller) Known(path string) bool { return c.index.Exists(path) }

// Overview summarizes the panel's sources for display.
type Overview struct {
	MapName       string
	Revision      uint64
	Types         int
	Instances     int
	Tiles         int
	Scripts       int
	Rules         int
	ScriptErrors  []updatepaths.ParseError
	Remembered    int
	Indexing      bool
	Analyzing     bool
	AnalysisError string
	IndexError    string
}

func (c *Controller) Overview() Overview {
	overview := Overview{
		MapName:       c.targetName,
		Revision:      c.installed.revision,
		Types:         len(c.inventory.Entries),
		Instances:     c.inventory.Instances,
		Tiles:         c.inventory.Tiles,
		Rules:         c.scripts.Len(),
		ScriptErrors:  c.scriptErrors,
		Remembered:    len(c.remembered),
		Indexing:      c.indexing,
		Analyzing:     c.running != nil,
		AnalysisError: c.analysisErr,
		IndexError:    c.indexError,
	}
	if c.scripts != nil {
		overview.Scripts = len(c.scripts.Scripts)
	}
	return overview
}

// Find shows the unknown path's instances in the search panel.
func (c *Controller) Find(path string) { c.app.ShowPathInSearch(path) }

// StatusCounts counts rows by display status.
func (c *Controller) StatusCounts() map[string]int {
	counts := map[string]int{}
	for _, row := range c.rows {
		counts[StatusOf(row)]++
	}
	return counts
}

func StatusOf(row Row) string {
	switch row.Choice.Kind {
	case ChoiceCandidate:
		if row.Choice.Human {
			return "Chosen"
		}
		return "Auto"
	case ChoiceCustom:
		return "Chosen"
	case ChoiceKeep:
		return "Kept"
	case ChoiceDelete:
		return "Delete"
	}
	if len(row.Proposal.Candidates) == 0 {
		return "No match"
	}
	return "Unresolved"
}

// Offering reports whether Certain results will be applied automatically to
// the map at path once it is analyzed.
func (c *Controller) Offering(path string) bool {
	return c.autoApplyAfter && c.autoApplyFor == path
}
