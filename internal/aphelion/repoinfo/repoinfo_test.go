package repoinfo

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const hash64 = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"

func TestValidateCommit(t *testing.T) {
	good := strings.Repeat("a1", 20)
	if err := ValidateCommit(good); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{"", good[:39], good + "0", strings.ToUpper(good), "g" + good[1:], good[:39] + " ", good[:39] + "\n", "-" + good[1:]} {
		if err := ValidateCommit(bad); err == nil {
			t.Errorf("commit %q accepted", bad)
		}
	}
}

func TestValidateBranch(t *testing.T) {
	for _, good := range []string{"main", "feature/play-test_1.2", "a", "release-1.0", "HEAD"} {
		if err := ValidateBranch(good); err != nil {
			t.Errorf("branch %q rejected: %v", good, err)
		}
	}
	long := strings.Repeat("a", MaxBranchLength+1)
	for _, bad := range []string{"", "-flag", "--detach", "a..b", "a b", "a;b", "a$b", "a`b", "a\nb", "a\\b", "a:b", "a~b", "a^b", "a?b", "a*b", "a[b", "a@{b", "/a", "a/", "a//b", "a.lock", "a/b.lock", ".a", "a/.b", "a.", long, "\u00e9"} {
		if err := ValidateBranch(bad); err == nil {
			t.Errorf("branch %q accepted", bad)
		}
	}
	if err := ValidateBranch(strings.Repeat("a", MaxBranchLength)); err != nil {
		t.Errorf("max length branch rejected: %v", err)
	}
}

func TestDescriptorValidate(t *testing.T) {
	commit := strings.Repeat("b", 40)
	good := Descriptor{DMEName: "tgstation.dme", EnvironmentHash: hash64, GitBranch: "main", GitCommit: commit}
	if err := good.Validate(); err != nil {
		t.Fatal(err)
	}
	if err := (Descriptor{DMEName: "x.dme", EnvironmentHash: hash64}).Validate(); err != nil {
		t.Fatalf("non-git descriptor: %v", err)
	}
	for name, bad := range map[string]Descriptor{
		"empty":        {},
		"no hash":      {DMEName: "x.dme"},
		"short hash":   {DMEName: "x.dme", EnvironmentHash: "abc"},
		"path name":    {DMEName: "dir/x.dme", EnvironmentHash: hash64},
		"windows path": {DMEName: `C:\x.dme`, EnvironmentHash: hash64},
		"backslash":    {DMEName: `a\x.dme`, EnvironmentHash: hash64},
		"url":          {DMEName: "https://x/y.dme", EnvironmentHash: hash64},
		"not dme":      {DMEName: "x.txt", EnvironmentHash: hash64},
		"control":      {DMEName: "x\n.dme", EnvironmentHash: hash64},
		"bad branch":   {DMEName: "x.dme", EnvironmentHash: hash64, GitBranch: "--x", GitCommit: commit},
		"bad commit":   {DMEName: "x.dme", EnvironmentHash: hash64, GitBranch: "main", GitCommit: "123"},
		"branch only":  {DMEName: "x.dme", EnvironmentHash: hash64, GitBranch: "main"},
	} {
		if err := bad.Validate(); err == nil {
			t.Errorf("%s accepted", name)
		}
	}
}

func TestCompare(t *testing.T) {
	sha1, sha2 := strings.Repeat("1", 40), strings.Repeat("2", 40)
	remote := Descriptor{DMEName: "x.dme", EnvironmentHash: hash64, GitBranch: "feature/x", GitCommit: sha1}
	tests := []struct {
		name   string
		remote Descriptor
		local  Local
		want   Status
	}{
		{"matches", remote, Local{Repository: true, Branch: "feature/x", Commit: sha1}, StatusMatches},
		{"different commit", remote, Local{Repository: true, Branch: "feature/x", Commit: sha2}, StatusDifferentCommit},
		{"different branch same commit", remote, Local{Repository: true, Branch: "main", Commit: sha1}, StatusDifferentBranch},
		{"detached same commit", remote, Local{Repository: true, Commit: sha1}, StatusDifferentBranch},
		{"dirty at same commit", remote, Local{Repository: true, Branch: "feature/x", Commit: sha1, Dirty: true}, StatusDirty},
		{"dirty with different commit", remote, Local{Repository: true, Branch: "main", Commit: sha2, Dirty: true}, StatusDifferentCommit},
		{"not a repository", remote, Local{}, StatusUnknown},
		{"host without git", Descriptor{DMEName: "x.dme", EnvironmentHash: hash64}, Local{Repository: true, Commit: sha1}, StatusUnknown},
		{"invalid host commit", Descriptor{DMEName: "x.dme", EnvironmentHash: hash64, GitCommit: "nope"}, Local{Repository: true, Commit: sha1}, StatusUnknown},
	}
	for _, test := range tests {
		if got := Compare(test.remote, test.local).Status; got != test.want {
			t.Errorf("%s: status %v, want %v", test.name, got, test.want)
		}
	}
}

func TestAlignmentCommands(t *testing.T) {
	sha1, sha2 := strings.Repeat("1", 40), strings.Repeat("2", 40)
	remote := Descriptor{DMEName: "x.dme", EnvironmentHash: hash64, GitBranch: "feature/x", GitCommit: sha1}
	different := Compare(remote, Local{Repository: true, Branch: "main", Commit: sha2})
	want := []string{"git fetch", "git switch --detach " + sha1}
	if got := different.Commands; strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("commands = %q, want %q", got, want)
	}
	if alt := different.BranchCommand; alt != "git switch feature/x" {
		t.Fatalf("branch command = %q", alt)
	}
	if text := different.CopyText(); !strings.Contains(text, "git fetch\n") || !strings.Contains(text, "git switch --detach "+sha1) {
		t.Fatalf("copy text = %q", text)
	}
	dirty := Compare(remote, Local{Repository: true, Branch: "main", Commit: sha2, Dirty: true})
	if !strings.HasPrefix(dirty.CopyText(), "# ") {
		t.Fatalf("dirty alignment must warn before commands: %q", dirty.CopyText())
	}
	if matches := Compare(remote, Local{Repository: true, Branch: "feature/x", Commit: sha1}); len(matches.Commands) != 0 || matches.CopyText() != "" {
		t.Fatal("matching checkout offered commands")
	}
	// Hostile branch names never reach the copyable text.
	hostile := Descriptor{DMEName: "x.dme", EnvironmentHash: hash64, GitBranch: "x;whoami", GitCommit: sha1}
	if text := Compare(hostile, Local{Repository: true, Commit: sha2}).CopyText(); text != "" {
		t.Fatalf("invalid descriptor produced commands: %q", text)
	}
}

func requireGit(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is unavailable")
	}
}

func gitIn(t *testing.T, dir string, args ...string) string {
	t.Helper()
	command := exec.Command("git", append([]string{"-c", "user.name=t", "-c", "user.email=t@example.invalid", "-c", "commit.gpgsign=false"}, args...)...)
	command.Dir = dir
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, output)
	}
	return strings.TrimSpace(string(output))
}

func newRepository(t *testing.T) string {
	requireGit(t)
	dir := t.TempDir()
	gitIn(t, dir, "init", "-q", "-b", "play-test")
	if err := os.WriteFile(filepath.Join(dir, "game.dme"), []byte("// env\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	gitIn(t, dir, "add", "game.dme")
	gitIn(t, dir, "commit", "-q", "-m", "initial")
	return dir
}

func TestInspectRealRepository(t *testing.T) {
	dir := newRepository(t)
	sub := filepath.Join(dir, "code")
	if err := os.Mkdir(sub, 0o700); err != nil {
		t.Fatal(err)
	}
	git := NewGit()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	local, err := git.Inspect(ctx, sub)
	if err != nil {
		t.Fatal(err)
	}
	if !local.Repository || local.Branch != "play-test" || local.Commit != gitIn(t, dir, "rev-parse", "HEAD") || local.Dirty {
		t.Fatalf("unexpected inspection %+v", local)
	}
	if err := os.WriteFile(filepath.Join(dir, "game.dme"), []byte("// changed\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if local, err = git.Inspect(ctx, dir); err != nil || !local.Dirty {
		t.Fatalf("modified worktree not dirty: %+v %v", local, err)
	}
	gitIn(t, dir, "checkout", "-q", "--detach")
	if local, err = git.Inspect(ctx, dir); err != nil || local.Branch != "" || local.Commit == "" {
		t.Fatalf("detached HEAD inspection %+v %v", local, err)
	}
}

func TestInspectOutsideRepositoryIsUnknownNotError(t *testing.T) {
	requireGit(t)
	dir := t.TempDir()
	t.Setenv("GIT_CEILING_DIRECTORIES", filepath.Dir(dir))
	local, err := NewGit().Inspect(context.Background(), dir)
	if err != nil || local.Repository {
		t.Fatalf("non-repository inspection = %+v, %v", local, err)
	}
}

func TestDescribeHostsRepository(t *testing.T) {
	dir := newRepository(t)
	descriptor, err := NewGit().Describe(context.Background(), filepath.Join(dir, "game.dme"), hash64)
	if err != nil {
		t.Fatal(err)
	}
	if descriptor.DMEName != "game.dme" || descriptor.EnvironmentHash != hash64 || descriptor.GitBranch != "play-test" || descriptor.GitCommit != gitIn(t, dir, "rev-parse", "HEAD") {
		t.Fatalf("descriptor = %+v", descriptor)
	}
	if err := descriptor.Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestDescribeDetachedHeadOmitsBranch(t *testing.T) {
	dir := newRepository(t)
	gitIn(t, dir, "checkout", "-q", "--detach")
	descriptor, err := NewGit().Describe(context.Background(), filepath.Join(dir, "game.dme"), hash64)
	if err != nil || descriptor.GitBranch != "" || descriptor.GitCommit == "" {
		t.Fatalf("descriptor = %+v, %v", descriptor, err)
	}
	if err := descriptor.Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestDescribeWithoutGitStillIdentifiesEnvironment(t *testing.T) {
	missing := &Git{} // No executable resolved.
	descriptor, err := missing.Describe(context.Background(), filepath.Join(t.TempDir(), "game.dme"), hash64)
	if err != nil {
		t.Fatal(err)
	}
	if descriptor.GitCommit != "" || descriptor.GitBranch != "" || descriptor.DMEName != "game.dme" {
		t.Fatalf("descriptor = %+v", descriptor)
	}
}

func TestInspectHonorsCancellation(t *testing.T) {
	requireGit(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := NewGit().Inspect(ctx, t.TempDir()); err == nil {
		t.Fatal("canceled inspection succeeded")
	}
}

func TestGitCommandsAreFixed(t *testing.T) {
	// The adapter accepts no caller-supplied arguments; keep the table closed.
	for _, args := range [][]string{revParseBranchArgs, revParseCommitArgs, statusArgs} {
		for _, argument := range args {
			if strings.ContainsAny(argument, " \t;&|<>$`\"'") {
				t.Fatalf("fixed argument %q contains shell metacharacters", argument)
			}
		}
	}
}
