package protocol

import "sdmm/internal/aphelion/repoinfo"

// Hosted discovery is control-plane data and never changes a map revision.
type HostedSessionMetadata struct {
	Visibility       string `json:"visibility"`
	Title            string `json:"title"`
	MapLabel         string `json:"map_label"`
	EnvironmentLabel string `json:"environment_label"`
}

type HostedSessionSummary struct {
	SessionID string `json:"session_id"`
	HostedSessionMetadata
	OwnerDisplayName string `json:"owner_display_name"`
	Participants     int    `json:"participants"`
	Available        bool   `json:"available"`
	// Repository is present only for clients that asked for it with IncludeRepositoryQuery.
	Repository *RepositoryDescriptor `json:"repository,omitempty"`
}

type HostedSessionsPage struct {
	Sessions   []HostedSessionSummary `json:"sessions"`
	NextCursor string                 `json:"next_cursor,omitempty"`
}

type HostedCapabilities struct {
	Provider       string `json:"provider"`
	SessionBrowser bool   `json:"session_browser"`
}

// RepositoryDescriptor is optional, additive and carries only a DME file name,
// environment hash and git branch/commit. Strict decoders in older peers reject
// unknown fields, so it is sent only after capability negotiation:
//   - the service advertises RepositoryCapabilityHeader on GET /v1/hosted/capabilities;
//   - hosts then add `repository` to the create request;
//   - clients then add include=repository to the session list query.
type RepositoryDescriptor = repoinfo.Descriptor

const (
	RepositoryCapabilityHeader = "X-Aphelion-Repository-Descriptor"
	RepositoryCapabilityValue  = "1"
	IncludeRepositoryQuery     = "repository"
)
