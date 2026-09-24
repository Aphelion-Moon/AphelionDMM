package server

import (
	"time"

	collabtelemetry "sdmm/internal/aphelion/collab/telemetry"
)

type DocumentConfig struct {
	BulkEdits bool
	// AppendReconciliationTimeout bounds resolving an ambiguous durable append
	// independently of the caller's upload or connection lifetime.
	AppendReconciliationTimeout time.Duration
	SnapshotOperationThreshold  int
	SnapshotInterval            time.Duration
	OnSnapshotError             func(error)
	Telemetry                   *collabtelemetry.Telemetry
}

func (config DocumentConfig) reconciliationTimeout() time.Duration {
	if config.AppendReconciliationTimeout > 0 {
		return config.AppendReconciliationTimeout
	}
	if config.BulkEdits {
		return 2 * time.Minute
	}
	return 5 * time.Second
}

type EmbeddedConfig struct {
	Store           SessionStore
	Document        DocumentConfig
	ListenAddress   string
	ReadTimeout     time.Duration
	WriteTimeout    time.Duration
	IdleTimeout     time.Duration
	ShutdownTimeout time.Duration
}

func (config EmbeddedConfig) withDefaults() EmbeddedConfig {
	if config.ListenAddress == "" {
		config.ListenAddress = "127.0.0.1:0"
	}
	if config.ReadTimeout <= 0 {
		config.ReadTimeout = 10 * time.Second
	}
	if config.WriteTimeout <= 0 {
		config.WriteTimeout = 10 * time.Second
	}
	if config.IdleTimeout <= 0 {
		config.IdleTimeout = 30 * time.Second
	}
	if config.ShutdownTimeout <= 0 {
		config.ShutdownTimeout = 10 * time.Second
	}
	return config
}
