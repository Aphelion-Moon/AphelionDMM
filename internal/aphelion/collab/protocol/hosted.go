package protocol

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
}

type HostedSessionsPage struct {
	Sessions   []HostedSessionSummary `json:"sessions"`
	NextCursor string                 `json:"next_cursor,omitempty"`
}

type HostedCapabilities struct {
	Provider       string `json:"provider"`
	SessionBrowser bool   `json:"session_browser"`
}
