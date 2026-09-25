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
