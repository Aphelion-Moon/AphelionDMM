// Package repoinfo reads and validates the small repository identity that
// collaborating editors compare before editing the same map.
//
// Every value is treated as hostile. The only external process is a single
// fixed git executable, resolved once, run with fixed arguments, no shell, a
// short deadline and bounded output. No caller-supplied text reaches a command.
package repoinfo

import (
	"fmt"
	"path/filepath"
	"strings"
	"unicode"
	"unicode/utf8"
)

const (
	// MaxBranchLength caps a branch name accepted from any source.
	MaxBranchLength = 200
	// MaxDMENameLength caps the DME file name carried in a descriptor.
	MaxDMENameLength = 128
	commitLength     = 40
	hashLength       = 64
)

// Descriptor is the public, additive collaboration contract. It carries no
// paths, remote URLs or credentials. Absent git fields mean "unknown".
type Descriptor struct {
	DMEName         string `json:"dme_name"`
	EnvironmentHash string `json:"environment_hash"`
	GitBranch       string `json:"git_branch,omitempty"`
	GitCommit       string `json:"git_commit,omitempty"`
}

// Validate applies the strict receipt rules. Branch alone is rejected because a
// commit is the only value that can be aligned deterministically.
func (d Descriptor) Validate() error {
	if err := ValidateDMEName(d.DMEName); err != nil {
		return err
	}
	if err := ValidateEnvironmentHash(d.EnvironmentHash); err != nil {
		return err
	}
	if d.GitCommit != "" {
		if err := ValidateCommit(d.GitCommit); err != nil {
			return err
		}
	}
	if d.GitBranch != "" {
		if err := ValidateBranch(d.GitBranch); err != nil {
			return err
		}
		if d.GitCommit == "" {
			return fmt.Errorf("git branch requires a git commit")
		}
	}
	return nil
}

// ValidateCommit accepts exactly a 40 digit lowercase SHA-1 object name.
func ValidateCommit(value string) error {
	if len(value) != commitLength || !lowerHex(value) {
		return fmt.Errorf("git commit must be %d lowercase hexadecimal characters", commitLength)
	}
	return nil
}

// ValidateEnvironmentHash accepts a 64 digit lowercase SHA-256.
func ValidateEnvironmentHash(value string) error {
	if len(value) != hashLength || !lowerHex(value) {
		return fmt.Errorf("environment hash must be %d lowercase hexadecimal characters", hashLength)
	}
	return nil
}

func lowerHex(value string) bool {
	for i := 0; i < len(value); i++ {
		character := value[i]
		if (character < '0' || character > '9') && (character < 'a' || character > 'f') {
			return false
		}
	}
	return true
}

// ValidateBranch accepts a conservative subset of git's ref grammar:
// [A-Za-z0-9._/-], at most MaxBranchLength bytes, never starting with "-".
func ValidateBranch(value string) error {
	if value == "" || len(value) > MaxBranchLength {
		return fmt.Errorf("git branch must be 1 through %d characters", MaxBranchLength)
	}
	if value[0] == '-' || value[0] == '/' || value[len(value)-1] == '/' || value[len(value)-1] == '.' {
		return fmt.Errorf("git branch has an invalid edge character")
	}
	if strings.Contains(value, "..") || strings.Contains(value, "//") {
		return fmt.Errorf("git branch contains an invalid sequence")
	}
	for i := 0; i < len(value); i++ {
		character := value[i]
		switch {
		case character >= 'a' && character <= 'z', character >= 'A' && character <= 'Z', character >= '0' && character <= '9':
		case character == '.', character == '_', character == '/', character == '-':
		default:
			return fmt.Errorf("git branch contains an unsupported character")
		}
	}
	for _, component := range strings.Split(value, "/") {
		if strings.HasPrefix(component, ".") || strings.HasSuffix(component, ".lock") {
			return fmt.Errorf("git branch component is invalid")
		}
	}
	return nil
}

// ValidateDMEName accepts a bare ".dme" file name: no separators, drive
// letters, URLs or control characters.
func ValidateDMEName(value string) error {
	if value == "" || len(value) > MaxDMENameLength || !utf8.ValidString(value) {
		return fmt.Errorf("DME name must be 1 through %d valid UTF-8 bytes", MaxDMENameLength)
	}
	if strings.ContainsAny(value, `/\:`) || value != strings.TrimSpace(value) || filepath.Base(value) != value {
		return fmt.Errorf("DME name must be a file name, not a path or URL")
	}
	for _, character := range value {
		if unicode.IsControl(character) {
			return fmt.Errorf("DME name contains a control character")
		}
	}
	if !strings.EqualFold(filepath.Ext(value), ".dme") || len(value) == len(".dme") {
		return fmt.Errorf("DME name must end in .dme")
	}
	return nil
}
