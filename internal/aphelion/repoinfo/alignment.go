package repoinfo

import "strings"

// Status is the joiner-facing comparison between a host descriptor and the
// local checkout of the loaded environment.
type Status int

const (
	StatusUnknown Status = iota
	StatusMatches
	StatusDifferentCommit
	StatusDifferentBranch
	StatusDirty
)

func (s Status) String() string {
	switch s {
	case StatusMatches:
		return "Matches the host's commit"
	case StatusDifferentCommit:
		return "Different commit from the host"
	case StatusDifferentBranch:
		return "Same commit, different branch"
	case StatusDirty:
		return "Uncommitted changes in the local worktree"
	default:
		return "Unknown"
	}
}

// Alignment is a read-only recommendation. The application never runs these
// commands; the user copies them into their own terminal.
type Alignment struct {
	Status Status
	// Dirty is reported even when the commit also differs.
	Dirty bool
	// Commands align the checkout to the host's exact commit.
	Commands []string
	// BranchCommand is an optional alternative that follows the host's branch.
	BranchCommand string
}

// Compare treats the remote descriptor as hostile: unvalidated values yield
// StatusUnknown and no commands.
func Compare(remote Descriptor, local Local) Alignment {
	if remote.GitCommit == "" || ValidateCommit(remote.GitCommit) != nil || !local.Repository || local.Commit == "" {
		return Alignment{Status: StatusUnknown}
	}
	if remote.GitBranch != "" && ValidateBranch(remote.GitBranch) != nil {
		return Alignment{Status: StatusUnknown}
	}
	result := Alignment{Dirty: local.Dirty}
	switch {
	case local.Commit != remote.GitCommit:
		result.Status = StatusDifferentCommit
		result.Commands = []string{"git fetch", "git switch --detach " + remote.GitCommit}
		if remote.GitBranch != "" {
			result.BranchCommand = "git switch " + remote.GitBranch
		}
	case local.Dirty:
		result.Status = StatusDirty
	case remote.GitBranch != "" && local.Branch != remote.GitBranch:
		result.Status = StatusDifferentBranch
	default:
		result.Status = StatusMatches
	}
	return result
}

// CopyText returns the commands for the clipboard, or "" when nothing needs
// changing. A dirty worktree gets a leading comment (valid in sh and PowerShell).
func (a Alignment) CopyText() string {
	if len(a.Commands) == 0 {
		return ""
	}
	var text strings.Builder
	if a.Dirty {
		text.WriteString("# Local changes exist: commit or stash them before switching.\n")
	}
	for _, command := range a.Commands {
		text.WriteString(command)
		text.WriteByte('\n')
	}
	if a.BranchCommand != "" {
		text.WriteString("# Or follow the host's branch instead:\n# ")
		text.WriteString(a.BranchCommand)
		text.WriteByte('\n')
	}
	return text.String()
}
