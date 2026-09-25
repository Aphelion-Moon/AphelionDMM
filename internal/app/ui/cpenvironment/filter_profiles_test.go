package cpenvironment

import (
	"testing"
	"time"

	"sdmm/internal/aphelion/filterprofiles"
	"sdmm/internal/app/config"
	"sdmm/internal/dmapi/dm"
	"sdmm/internal/dmapi/dmenv"
)

type profileApp struct {
	App
	env    *dmenv.Dme
	filter *dm.PathsFilter
	cfg    *cpenvironmentConfig
	queue  chan func()
}

func (a *profileApp) LoadedEnvironment() *dmenv.Dme   { return a.env }
func (a *profileApp) PathsFilter() *dm.PathsFilter    { return a.filter }
func (a *profileApp) ConfigFind(string) config.Config { return a.cfg }
func (a *profileApp) RunLater(f func())               { a.queue <- f }
func publishProfile(t *testing.T, a *profileApp) {
	t.Helper()
	select {
	case f := <-a.queue:
		f()
	case <-time.After(5 * time.Second):
		t.Fatal("profile compile did not publish")
	}
}
func profileFixture() (*Environment, *profileApp) {
	a := &profileApp{env: &dmenv.Dme{RootFile: "test.dme", Objects: map[string]*dmenv.Object{"/obj": {Path: "/obj"}}}, filter: dm.NewPathsFilterEmpty(), cfg: &cpenvironmentConfig{FilterProfiles: filterprofiles.NewStore(), ActiveProfileIDs: map[string]string{}}, queue: make(chan func(), 4)}
	return &Environment{app: a}, a
}
func TestProfilePublicationFencesAndRestore(t *testing.T) {
	e, a := profileFixture()
	e.BindFilterEnvironment(a.env)
	publishProfile(t, a)
	profile := filterprofiles.Profile{ID: "custom:test", Name: "Hidden objects", DefaultVisible: true, Rules: []filterprofiles.Rule{{Path: "/obj", Scope: filterprofiles.ScopeExact, Visible: false}}}
	if err := a.cfg.FilterProfiles.Add(profile); err != nil {
		t.Fatal(err)
	}
	initial := a.filter.PolicyRevision()
	e.compileFilterProfile(profile, true)
	if a.filter.IsHiddenPath("/obj") {
		t.Fatal("policy changed before publication")
	}
	publishProfile(t, a)
	if !a.filter.IsHiddenPath("/obj") || a.filter.PolicyRevision() != initial+1 {
		t.Fatal("profile did not publish exactly once")
	}
	key := e.filterProfileProjectKey
	e.invalidateFilterProfiles()
	a.filter.Clear()
	e.BindFilterEnvironment(a.env)
	publishProfile(t, a)
	if !a.filter.IsHiddenPath("/obj") || a.cfg.ActiveProfileIDs[key] != profile.ID {
		t.Fatal("saved profile was not restored")
	}
	e.compileFilterProfile(filterprofiles.DefaultProfile(), true)
	e.invalidateFilterProfiles()
	publishProfile(t, a)
	if !a.filter.IsHiddenPath("/obj") || a.cfg.ActiveProfileIDs[key] != profile.ID {
		t.Fatal("stale profile published after environment close")
	}
}
func TestRenameProfilePreservesTemporaryVisibility(t *testing.T) {
	e, a := profileFixture()
	e.BindFilterEnvironment(a.env)
	publishProfile(t, a)
	profile := filterprofiles.Profile{ID: "custom:test", Name: "Original", DefaultVisible: true}
	if err := a.cfg.FilterProfiles.Add(profile); err != nil {
		t.Fatal(err)
	}
	e.compileFilterProfile(profile, true)
	publishProfile(t, a)
	if err := e.SetFilterVisibility("/obj", filterprofiles.ScopeExact, false); err != nil {
		t.Fatal(err)
	}
	publishProfile(t, a)
	revision := a.filter.PolicyRevision()
	e.filterProfileName = "Renamed"
	e.renameFilterProfile(profile)
	if e.filterCompilePending {
		publishProfile(t, a)
	}
	if !a.filter.IsHiddenPath("/obj") || !e.filterProfiles.OverridesDirty() || a.filter.PolicyRevision() != revision || e.activeProfile().Name != "Renamed" {
		t.Fatal("rename changed temporary visibility")
	}
}

func TestVisibilityCommandsPreserveAdmittedHidesAndFenceReset(t *testing.T) {
	e, a := profileFixture()
	a.env.Objects["/obj/child"] = &dmenv.Object{Path: "/obj/child"}
	a.env.Objects["/obj/other"] = &dmenv.Object{Path: "/obj/other"}
	a.env.Objects["/obj"].DirectChildren = []string{"/obj/child", "/obj/other"}
	e.BindFilterEnvironment(a.env)
	publishProfile(t, a)
	e.compileFilterProfile(filterprofiles.DefaultProfile(), false)
	for _, path := range []string{"/obj", "/obj/other"} {
		if err := e.SetFilterVisibility(path, filterprofiles.ScopeExact, false); err != nil {
			t.Fatalf("hide rejected while policy work was in flight: %v", err)
		}
	}
	for e.filterCompilePending {
		publishProfile(t, a)
	}
	if !a.filter.IsHiddenPath("/obj") || !a.filter.IsHiddenPath("/obj/other") || !a.filter.IsVisiblePath("/obj/child") {
		t.Fatal("successive admitted hides were lost or expanded to descendants")
	}
	e.compileFilterProfile(filterprofiles.DefaultProfile(), false)
	if err := e.SetFilterVisibility("/obj/child", filterprofiles.ScopeExact, false); err != nil {
		t.Fatal(err)
	}
	if err := e.ShowAllFilterVisibility(); err != nil {
		t.Fatal(err)
	}
	for e.filterCompilePending {
		publishProfile(t, a)
	}
	if len(a.filter.HiddenPaths()) != 0 {
		t.Fatal("older policy work undid Show All")
	}
}

func TestExactHiddenParentRetainsMixedTreeSummary(t *testing.T) {
	e, a := profileFixture()
	a.env.Objects["/obj/child"] = &dmenv.Object{Path: "/obj/child"}
	a.env.Objects["/obj"].DirectChildren = []string{"/obj/child"}
	e.BindFilterEnvironment(a.env)
	publishProfile(t, a)
	if err := e.SetFilterVisibility("/obj", filterprofiles.ScopeExact, false); err != nil {
		t.Fatal(err)
	}
	publishProfile(t, a)
	checked, mixed := e.treeVisibility("/obj")
	if checked || !mixed {
		t.Fatal("exact hidden parent misrepresented its visible descendants")
	}
}

func TestMissingHiddenPathDoesNotCountAsTreeDescendant(t *testing.T) {
	e, a := profileFixture()
	a.env.Objects["/obj/child"] = &dmenv.Object{Path: "/obj/child"}
	a.env.Objects["/obj"].DirectChildren = []string{"/obj/child"}
	e.BindFilterEnvironment(a.env)
	publishProfile(t, a)
	profile := filterprofiles.DefaultProfile()
	profile.Rules = []filterprofiles.Rule{{Path: "/obj/removed", Scope: filterprofiles.ScopeExact, Visible: false}}
	e.compileFilterProfile(profile, false)
	publishProfile(t, a)
	if checked, mixed := e.treeVisibility("/obj"); !checked || mixed {
		t.Fatal("missing type distorted visible tree")
	}
	if err := e.SetFilterVisibility("/obj", filterprofiles.ScopeExact, false); err != nil {
		t.Fatal(err)
	}
	publishProfile(t, a)
	if checked, mixed := e.treeVisibility("/obj"); checked || !mixed {
		t.Fatal("missing type concealed visible descendant")
	}
}

func TestVisibilityHistoryRestoresPersistedProfileChoice(t *testing.T) {
	e, a := profileFixture()
	e.BindFilterEnvironment(a.env)
	publishProfile(t, a)
	first := filterprofiles.Profile{ID: "custom:first", Name: "First", DefaultVisible: true}
	if err := a.cfg.FilterProfiles.Add(first); err != nil {
		t.Fatal(err)
	}
	e.compileFilterProfile(first, true)
	publishProfile(t, a)
	initial := first.ID
	profile := filterprofiles.Profile{ID: "custom:history", Name: "History", DefaultVisible: false}
	if err := a.cfg.FilterProfiles.Add(profile); err != nil {
		t.Fatal(err)
	}
	e.compileFilterProfile(profile, true)
	publishProfile(t, a)
	if err := e.enqueueVisibility(visibilityCommand{kind: "undo-visibility"}); err != nil {
		t.Fatal(err)
	}
	publishProfile(t, a)
	if a.cfg.ActiveProfileIDs[e.filterProfileProjectKey] != initial {
		t.Fatal("undo left saved project choice at the newer profile")
	}
}
