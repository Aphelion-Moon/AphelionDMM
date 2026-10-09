package ui

import (
	"fmt"
	"strings"

	"sdmm/internal/aphelion/collab/model"
	"sdmm/internal/aphelion/collab/protocol"
)

// EnvironmentMismatchError is returned when a joined snapshot was authored
// against a different environment than the one loaded locally. Environment
// hashes are protocol inputs, so the session is refused rather than adapted.
type EnvironmentMismatchError struct {
	LocalName   string
	LocalHash   string
	SessionHash string
	Host        *protocol.RepositoryDescriptor
}

func (e *EnvironmentMismatchError) Error() string {
	var text strings.Builder
	text.WriteString("The loaded environment does not match this session.\n")
	name := e.LocalName
	if name == "" {
		name = "(unnamed)"
	}
	fmt.Fprintf(&text, "Your environment: %s (hash %s)\n", name, ShortHash(e.LocalHash, 12))
	fmt.Fprintf(&text, "Session environment hash: %s\n", ShortHash(e.SessionHash, 12))
	text.WriteString(DescribeRepository(e.Host))
	text.WriteString("\nAlign your checkout with the host, reload the environment, then join again.")
	return text.String()
}

// CheckJoinedEnvironment verifies a received snapshot against the loaded
// environment before any document is built from it.
func CheckJoinedEnvironment(snapshot model.Snapshot, localHash, localName string, host *protocol.RepositoryDescriptor) error {
	if localHash == "" || snapshot.EnvironmentHash != localHash {
		return &EnvironmentMismatchError{LocalName: localName, LocalHash: localHash, SessionHash: snapshot.EnvironmentHash, Host: host}
	}
	return nil
}

// UntitledName names a session-owned document that has no file yet.
func UntitledName(documentID model.DocumentID) string {
	return "Untitled-" + ShortHash(strings.ReplaceAll(string(documentID), "-", ""), 8) + ".dmm"
}
