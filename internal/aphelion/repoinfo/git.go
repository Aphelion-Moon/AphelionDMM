package repoinfo

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const (
	commandTimeout = 5 * time.Second
	outputLimit    = 64 << 10
)

// The complete set of subcommands this package can run. They contain no
// placeholders; nothing from a descriptor, map or user reaches them.
var (
	revParseBranchArgs = []string{"rev-parse", "--abbrev-ref", "HEAD"}
	revParseCommitArgs = []string{"rev-parse", "HEAD"}
	// core.fsmonitor=false prevents repository configuration from launching a hook.
	statusArgs = []string{"-c", "core.fsmonitor=false", "status", "--porcelain"}
)

// Git is a fixed-executable adapter. The zero value has no executable and
// reports every checkout as unknown.
type Git struct {
	executable string
}

var defaultGit = sync.OnceValue(NewGit)

// Default resolves git once for the process.
func Default() *Git { return defaultGit() }

// NewGit resolves git on PATH. A missing git, or one found relative to the
// working directory, leaves the adapter unavailable instead of failing.
func NewGit() *Git {
	path, err := exec.LookPath("git")
	if err != nil { // Includes exec.ErrDot: never run a git found in the working directory.
		return &Git{}
	}
	if absolute, absErr := filepath.Abs(path); absErr == nil {
		path = absolute
	}
	return &Git{executable: path}
}

// Available reports whether a git executable was resolved.
func (g *Git) Available() bool { return g != nil && g.executable != "" }

// Local describes a checkout. Repository is false when git is unavailable or
// the directory is not inside a worktree. Branch is empty for a detached HEAD.
type Local struct {
	Repository bool
	Branch     string
	Commit     string
	Dirty      bool
}

// Inspect reads the commit, branch and worktree cleanliness for dir.
func (g *Git) Inspect(ctx context.Context, dir string) (Local, error) {
	local, err := g.head(ctx, dir)
	if err != nil || !local.Repository {
		return local, err
	}
	status, code, err := g.run(ctx, dir, statusArgs)
	if err != nil {
		return Local{}, err
	}
	if code != 0 {
		return Local{}, fmt.Errorf("git status exited with code %d", code)
	}
	local.Dirty = len(bytes.TrimSpace(status)) != 0
	return local, nil
}

// Describe builds the host descriptor for a loaded DME. Git details are best
// effort: any failure leaves them empty rather than blocking collaboration.
func (g *Git) Describe(ctx context.Context, dmePath, environmentHash string) (Descriptor, error) {
	descriptor := Descriptor{DMEName: filepath.Base(dmePath), EnvironmentHash: environmentHash}
	if local, err := g.head(ctx, filepath.Dir(dmePath)); err == nil && local.Repository {
		descriptor.GitCommit, descriptor.GitBranch = local.Commit, local.Branch
	}
	if err := descriptor.Validate(); err != nil {
		return Descriptor{}, err
	}
	return descriptor, nil
}

func (g *Git) head(ctx context.Context, dir string) (Local, error) {
	if !g.Available() {
		return Local{}, ctx.Err()
	}
	commit, code, err := g.run(ctx, dir, revParseCommitArgs)
	if err != nil {
		return Local{}, err
	}
	if code != 0 {
		return Local{}, nil // Not a worktree, or an unborn branch.
	}
	local := Local{Repository: true}
	if value := strings.TrimSpace(string(commit)); ValidateCommit(value) == nil {
		local.Commit = value
	} else {
		return Local{}, nil
	}
	branch, code, err := g.run(ctx, dir, revParseBranchArgs)
	if err != nil {
		return Local{}, err
	}
	if value := strings.TrimSpace(string(branch)); code == 0 && value != "HEAD" && ValidateBranch(value) == nil {
		local.Branch = value
	}
	return local, nil
}

// run returns stdout and the exit code. Only failures to run the process, or
// cancellation, are errors; a non-zero exit is data.
func (g *Git) run(ctx context.Context, dir string, args []string) ([]byte, int, error) {
	if err := ctx.Err(); err != nil {
		return nil, 0, err
	}
	info, err := os.Stat(dir)
	if err != nil || !info.IsDir() {
		return nil, 0, fmt.Errorf("repository directory is unavailable")
	}
	ctx, cancel := context.WithTimeout(ctx, commandTimeout)
	defer cancel()
	command := exec.CommandContext(ctx, g.executable, args...)
	command.Dir = dir
	command.Env = append(os.Environ(), "GIT_OPTIONAL_LOCKS=0", "GIT_TERMINAL_PROMPT=0", "LC_ALL=C")
	command.Stdin = nil
	output := &limitedBuffer{limit: outputLimit}
	command.Stdout = output
	command.Stderr = nil
	configureProcess(command)
	err = command.Run()
	var exit *exec.ExitError
	switch {
	case err == nil:
		return output.Bytes(), 0, nil
	case ctx.Err() != nil:
		return nil, 0, ctx.Err()
	case errors.As(err, &exit):
		return output.Bytes(), exit.ExitCode(), nil
	default:
		return nil, 0, fmt.Errorf("run git: %w", err)
	}
}

type limitedBuffer struct {
	bytes.Buffer
	limit int
}

func (b *limitedBuffer) Write(p []byte) (int, error) {
	if remaining := b.limit - b.Len(); remaining > 0 {
		b.Buffer.Write(p[:min(len(p), remaining)])
	}
	return len(p), nil
}
