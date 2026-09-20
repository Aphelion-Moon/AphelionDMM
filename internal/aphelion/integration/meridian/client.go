package meridian

import (
	"context"
	"encoding/json"
	"time"
)

// Client exposes only the Meridian-MCP operations required for staged map verification.
type Client interface {
	ParseEnvironment(ctx context.Context, repositoryID, dmeID string) (EnvironmentResult, error)
	InspectMap(ctx context.Context, repositoryID, mapTargetID string) (MapResult, error)
	CheckErrors(ctx context.Context, repositoryID string) (DiagnosticResult, error)
	Close(ctx context.Context) error
}

// Repository maps logical integration identifiers to one trusted local root.
type Repository struct {
	Identity string
	Root     string
	DME      string
	DMEPath  string
	Targets  map[string]string
}

// Config is immutable trusted local process and repository configuration.
type Config struct {
	Executable  string
	Arguments   []string
	Environment map[string]string
	Roots       map[string]Repository
	Timeout     time.Duration
	MaxBytes    int64
}

// EnvironmentResult records the active parsed environment generation.
type EnvironmentResult struct {
	RepositoryID    string
	DMEIdentifier   string
	StateGeneration uint64
	MCPVersion      string
	TotalTypes      uint64
	IndexedSymbols  uint64
	Raw             json.RawMessage
}

// MapResult records bounded map metadata at the active environment generation.
type MapResult struct {
	RepositoryID    string
	MapTargetID     string
	StateGeneration uint64
	MCPVersion      string
	Width           uint64
	Height          uint64
	Levels          uint64
	Raw             json.RawMessage
}

// DiagnosticResult records bounded diagnostics at the active environment generation.
type DiagnosticResult struct {
	RepositoryID    string
	StateGeneration uint64
	MCPVersion      string
	// Count is the complete reported total, or an unpaginated legacy count.
	Count          uint64
	ReturnedCount  uint64
	Truncated      bool
	SeverityCounts map[string]uint64
	Diagnostics    json.RawMessage
	Raw            json.RawMessage
}
