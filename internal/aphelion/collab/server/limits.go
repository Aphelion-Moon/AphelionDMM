package server

import (
	"time"

	"sdmm/internal/aphelion/collab/protocol"
)

const (
	defaultMaxConnections       = 256
	defaultRateEntries          = 4096
	defaultDurableQueueDepth    = 64
	defaultPresenceQueueDepth   = 1
	defaultPresenceAbuseWindows = 5
)

type RateLimit struct {
	Burst  int
	Window time.Duration
}

type Limits struct {
	MaxConnections           int
	MaxOperationChanges      int
	MaxWebSocketMessageBytes int64
	MaxHTTPBodyBytes         int64
	MaxSnapshotBodyBytes     int64
	DurableQueueDepth        int
	PresenceQueueDepth       int
	RateEntries              int
	JoinRate                 RateLimit
	DurableRate              RateLimit
	PresenceRate             RateLimit
	// PresenceAbuseWindows is how many consecutive PresenceRate windows a single
	// connection may exceed before it is closed with CloseRateLimited. Excess
	// presence inside a tolerated window is dropped, not applied.
	PresenceAbuseWindows int
}

func DefaultLimits() Limits {
	return Limits{
		MaxConnections:           defaultMaxConnections,
		MaxOperationChanges:      protocol.MaxOperationChanges,
		MaxWebSocketMessageBytes: protocol.MaxMessageBytes,
		MaxHTTPBodyBytes:         MaxHTTPBodyBytes,
		MaxSnapshotBodyBytes:     MaxSnapshotBodyBytes,
		DurableQueueDepth:        defaultDurableQueueDepth,
		PresenceQueueDepth:       defaultPresenceQueueDepth,
		RateEntries:              defaultRateEntries,
		JoinRate:                 RateLimit{Burst: 30, Window: time.Minute},
		DurableRate:              RateLimit{Burst: 120, Window: time.Second},
		PresenceRate:             RateLimit{Burst: 120, Window: time.Second},
		PresenceAbuseWindows:     defaultPresenceAbuseWindows,
	}
}

func (limits Limits) withDefaults() Limits {
	defaults := DefaultLimits()
	if limits.MaxConnections <= 0 {
		limits.MaxConnections = defaults.MaxConnections
	}
	if limits.MaxOperationChanges <= 0 || limits.MaxOperationChanges > protocol.MaxOperationChanges {
		limits.MaxOperationChanges = defaults.MaxOperationChanges
	}
	if limits.MaxWebSocketMessageBytes <= 0 || limits.MaxWebSocketMessageBytes > protocol.MaxMessageBytes {
		limits.MaxWebSocketMessageBytes = defaults.MaxWebSocketMessageBytes
	}
	if limits.MaxHTTPBodyBytes <= 0 || limits.MaxHTTPBodyBytes > MaxHTTPBodyBytes {
		limits.MaxHTTPBodyBytes = defaults.MaxHTTPBodyBytes
	}
	if limits.MaxSnapshotBodyBytes <= 0 || limits.MaxSnapshotBodyBytes > MaxSnapshotBodyBytes {
		limits.MaxSnapshotBodyBytes = defaults.MaxSnapshotBodyBytes
	}
	if limits.DurableQueueDepth <= 0 {
		limits.DurableQueueDepth = defaults.DurableQueueDepth
	}
	if limits.PresenceQueueDepth <= 0 {
		limits.PresenceQueueDepth = defaults.PresenceQueueDepth
	}
	if limits.RateEntries <= 0 {
		limits.RateEntries = defaults.RateEntries
	}
	limits.JoinRate = limits.JoinRate.withDefault(defaults.JoinRate)
	limits.DurableRate = limits.DurableRate.withDefault(defaults.DurableRate)
	limits.PresenceRate = limits.PresenceRate.withDefault(defaults.PresenceRate)
	if limits.PresenceAbuseWindows <= 0 {
		limits.PresenceAbuseWindows = defaults.PresenceAbuseWindows
	}
	return limits
}

func (limit RateLimit) withDefault(fallback RateLimit) RateLimit {
	if limit.Burst <= 0 || limit.Window <= 0 {
		return fallback
	}
	return limit
}
