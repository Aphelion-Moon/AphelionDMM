package ui

import (
	"context"
	"errors"
	"fmt"
	"time"

	"sdmm/internal/aphelion/collab/executor"
	"sdmm/internal/aphelion/collab/model"
)

const incompleteSessionCleanupTimeout = 5 * time.Second

var ErrAttachmentTargetChanged = errors.New("collaboration attachment target changed")

type LocalSessionLifecycle interface {
	CreateLocal(context.Context, model.Snapshot) error
	Leave(context.Context) error
}

type NamedLocalSessionLifecycle interface {
	CreateLocalNamed(context.Context, model.Snapshot, string) error
	Leave(context.Context) error
}

type JoinedSessionLifecycle interface {
	Join(context.Context, Invitation) error
	Leave(context.Context) error
}

type HostedSessionLifecycle interface {
	JoinHosted(context.Context, HostedConnection) error
	Leave(context.Context) error
}

func PrepareHostedSession(ctx context.Context, lifecycle HostedSessionLifecycle, provider CollaborationExecutorProvider, target HostedConnection) (executor.Executor, error) {
	if err := lifecycle.JoinHosted(ctx, target); err != nil {
		return nil, err
	}
	if execution := provider.CollaborationExecutor(); execution != nil {
		return execution, nil
	}
	cleanup, cancel := context.WithTimeout(context.Background(), incompleteSessionCleanupTimeout)
	defer cancel()
	return nil, errors.Join(fmt.Errorf("synchronized hosted executor is unavailable"), lifecycle.Leave(cleanup))
}

type CollaborationExecutorProvider interface {
	CollaborationExecutor() executor.Executor
}

type CollaborationAttachmentTarget interface {
	AttachCollaborationExecutor(executor.Executor) error
}

// PrepareLocalSession creates a session and returns its synchronized executor for UI-thread attachment.
func PrepareLocalSession(ctx context.Context, lifecycle LocalSessionLifecycle, provider CollaborationExecutorProvider, snapshot model.Snapshot) (executor.Executor, error) {
	if lifecycle == nil || provider == nil {
		return nil, fmt.Errorf("prepare local collaboration session: lifecycle is unavailable")
	}
	if err := lifecycle.CreateLocal(ctx, snapshot); err != nil {
		return nil, err
	}
	execution := provider.CollaborationExecutor()
	if execution != nil {
		return execution, nil
	}
	cleanupContext, cancelCleanup := context.WithTimeout(context.Background(), incompleteSessionCleanupTimeout)
	defer cancelCleanup()
	cleanupErr := lifecycle.Leave(cleanupContext)
	return nil, errors.Join(fmt.Errorf("prepare local collaboration session: synchronized executor is unavailable"), cleanupErr)
}

func PrepareNamedLocalSession(ctx context.Context, lifecycle NamedLocalSessionLifecycle, provider CollaborationExecutorProvider, snapshot model.Snapshot, displayName string) (executor.Executor, error) {
	if lifecycle == nil || provider == nil {
		return nil, fmt.Errorf("prepare local collaboration session: lifecycle is unavailable")
	}
	if err := lifecycle.CreateLocalNamed(ctx, snapshot, displayName); err != nil {
		return nil, err
	}
	execution := provider.CollaborationExecutor()
	if execution != nil {
		return execution, nil
	}
	cleanupContext, cancelCleanup := context.WithTimeout(context.Background(), incompleteSessionCleanupTimeout)
	defer cancelCleanup()
	cleanupErr := lifecycle.Leave(cleanupContext)
	return nil, errors.Join(fmt.Errorf("prepare local collaboration session: synchronized executor is unavailable"), cleanupErr)
}

// PrepareJoinedSession joins a session and returns its synchronized executor for UI-thread attachment.
func PrepareJoinedSession(ctx context.Context, lifecycle JoinedSessionLifecycle, provider CollaborationExecutorProvider, invitation Invitation) (executor.Executor, error) {
	if lifecycle == nil || provider == nil {
		return nil, fmt.Errorf("prepare joined collaboration session: lifecycle is unavailable")
	}
	if err := lifecycle.Join(ctx, invitation); err != nil {
		return nil, err
	}
	execution := provider.CollaborationExecutor()
	if execution != nil {
		return execution, nil
	}
	cleanupContext, cancelCleanup := context.WithTimeout(context.Background(), incompleteSessionCleanupTimeout)
	defer cancelCleanup()
	cleanupErr := lifecycle.Leave(cleanupContext)
	return nil, errors.Join(fmt.Errorf("prepare joined collaboration session: synchronized executor is unavailable"), cleanupErr)
}

// AttachPreparedSession installs a synchronized executor only if the originating editor is still current.
func AttachPreparedSession(execution executor.Executor, target CollaborationAttachmentTarget, stillCurrent bool) error {
	if !stillCurrent {
		return ErrAttachmentTargetChanged
	}
	if execution == nil || target == nil {
		return fmt.Errorf("attach prepared collaboration session: target or executor is unavailable")
	}
	return target.AttachCollaborationExecutor(execution)
}
