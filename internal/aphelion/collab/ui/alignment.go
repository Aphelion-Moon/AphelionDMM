package ui

import (
	"fmt"

	"sdmm/internal/aphelion/collab/protocol"
	"sdmm/internal/aphelion/repoinfo"
)

// LocalEnvironment identifies the loaded environment and its local checkout.
// It carries a DME file name and hashes only; local paths stay in the app.
type LocalEnvironment struct {
	DMEName         string
	EnvironmentHash string
	Repository      repoinfo.Local
}

// AlignmentReport is the joiner's read-only comparison of a host descriptor
// with the local checkout. Commands are for the user's own terminal.
type AlignmentReport struct {
	EnvironmentMatches bool
	Alignment          repoinfo.Alignment
}

// BuildAlignmentReport treats the host descriptor as hostile data.
func BuildAlignmentReport(host protocol.RepositoryDescriptor, local LocalEnvironment) AlignmentReport {
	report := AlignmentReport{EnvironmentMatches: local.EnvironmentHash != "" && local.EnvironmentHash == host.EnvironmentHash}
	if host.Validate() != nil {
		return report
	}
	report.Alignment = repoinfo.Compare(host, local.Repository)
	return report
}

// ShortHash abbreviates a hash for labels; it never returns more than n runes.
func ShortHash(value string, n int) string {
	if len(value) <= n {
		return value
	}
	return value[:n]
}

// DescribeRepository renders a descriptor for labels and mismatch dialogs.
func DescribeRepository(descriptor *protocol.RepositoryDescriptor) string {
	if descriptor == nil || descriptor.Validate() != nil {
		return "host repository: unknown"
	}
	text := fmt.Sprintf("host environment: %s (hash %s)", descriptor.DMEName, ShortHash(descriptor.EnvironmentHash, 12))
	if descriptor.GitCommit != "" {
		text += fmt.Sprintf("\nhost commit: %s", ShortHash(descriptor.GitCommit, 12))
		if descriptor.GitBranch != "" {
			text += fmt.Sprintf(" on %s", descriptor.GitBranch)
		}
	}
	return text
}
