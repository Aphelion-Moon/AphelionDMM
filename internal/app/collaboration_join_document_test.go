package app

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"sdmm/internal/dmapi/dmenv"
)

func TestJoinNewTabNeedsEnvironmentButNotMap(t *testing.T) {
	a := &app{}
	if a.collaborationJoinNewTabBlocker() == "" {
		t.Fatal("joining without a loaded environment was allowed")
	}
	a.loadedEnvironment = &dmenv.Dme{}
	if reason := a.collaborationJoinNewTabBlocker(); reason != "" {
		t.Fatalf("environment-only join blocked: %s", reason)
	}
}

func repositoryForApp(t *testing.T) (*dmenv.Dme, string) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is unavailable")
	}
	dir := t.TempDir()
	run := func(args ...string) string {
		command := exec.Command("git", append([]string{"-c", "user.name=t", "-c", "user.email=t@example.invalid", "-c", "commit.gpgsign=false"}, args...)...)
		command.Dir = dir
		output, err := command.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, output)
		}
		return strings.TrimSpace(string(output))
	}
	run("init", "-q", "-b", "play-test")
	if err := os.WriteFile(filepath.Join(dir, "game.dme"), []byte("// env\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	run("add", "game.dme")
	run("commit", "-q", "-m", "initial")
	return &dmenv.Dme{RootDir: dir, RootFile: filepath.Join(dir, "game.dme")}, run("rev-parse", "HEAD")
}

func TestLocalInspectionExposesOnlyNameHashesAndCheckoutState(t *testing.T) {
	environment, commit := repositoryForApp(t)
	a := &app{loadedEnvironment: environment}
	inspect := a.collaborationLocalInspection()
	// The inspection is bound to the environment loaded now, not later.
	a.loadedEnvironment = nil
	local, err := inspect(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if local.DMEName != "game.dme" || len(local.EnvironmentHash) != 64 || !local.Repository.Repository || local.Repository.Commit != commit || local.Repository.Branch != "play-test" || local.Repository.Dirty {
		t.Fatalf("inspection = %+v", local)
	}
	if err := os.WriteFile(filepath.Join(environment.RootDir, "game.dme"), []byte("// changed\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if local, err = inspect(context.Background()); err != nil || !local.Repository.Dirty {
		t.Fatalf("modified checkout not reported dirty: %+v %v", local, err)
	}
	empty := (&app{}).collaborationLocalInspection()
	if _, err := empty(context.Background()); err == nil {
		t.Fatal("inspection without an environment succeeded")
	}
}

func TestHostRepositoryDescriptorIsValidAndPathFree(t *testing.T) {
	environment, commit := repositoryForApp(t)
	hash := strings.Repeat("a", 64)
	descriptor := hostRepositoryDescriptor(context.Background(), environment, hash)
	if descriptor == nil || descriptor.GitCommit != commit || descriptor.GitBranch != "play-test" || descriptor.DMEName != "game.dme" || descriptor.EnvironmentHash != hash {
		t.Fatalf("descriptor = %+v", descriptor)
	}
	if err := descriptor.Validate(); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(descriptor.DMEName, string(filepath.Separator)) {
		t.Fatal("descriptor carries a path")
	}
	if hostRepositoryDescriptor(context.Background(), nil, hash) != nil {
		t.Fatal("descriptor produced without an environment")
	}
	// Outside a worktree the environment is still identified, with git unknown.
	plain := t.TempDir()
	t.Setenv("GIT_CEILING_DIRECTORIES", filepath.Dir(plain))
	descriptor = hostRepositoryDescriptor(context.Background(), &dmenv.Dme{RootDir: plain, RootFile: filepath.Join(plain, "x.dme")}, hash)
	if descriptor == nil || descriptor.GitCommit != "" || descriptor.GitBranch != "" || descriptor.DMEName != "x.dme" {
		t.Fatalf("non-git descriptor = %+v", descriptor)
	}
}
