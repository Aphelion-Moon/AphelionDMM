package client

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/coder/websocket"

	"sdmm/internal/aphelion/collab/model"
)

var (
	ErrAuthenticationDenied = errors.New("collaboration authentication was denied")
	ErrIncompatibleProtocol = errors.New("collaboration protocol is incompatible")
)

// closeRateLimited mirrors the server's 4429 close code; the client does not
// import the server package.
const closeRateLimited websocket.StatusCode = 4429

// ErrRateLimited marks a connection the server closed for exceeding a rate
// limit. It is recoverable: the session reconnects with backoff.
var ErrRateLimited = errors.New("collaboration connection was rate limited by the server; reconnecting")

// ClassifyCloseError wraps a 4429 close with ErrRateLimited while preserving
// the original error, so websocket.CloseStatus still reports the code.
func ClassifyCloseError(err error) error {
	if err == nil || errors.Is(err, ErrRateLimited) || websocket.CloseStatus(err) != closeRateLimited {
		return err
	}
	return fmt.Errorf("%w: %w", ErrRateLimited, err)
}

type permanentReconnectError struct {
	err error
}

func (reconnectError permanentReconnectError) Error() string { return reconnectError.err.Error() }

func (reconnectError permanentReconnectError) Unwrap() error { return reconnectError.err }

func PermanentReconnectError(err error) error {
	if err == nil {
		return nil
	}
	return permanentReconnectError{err: err}
}

type ReconnectPolicy struct {
	InitialDelay time.Duration
	MaxDelay     time.Duration
	Jitter       float64
	MaxAttempts  int
	Random       func() float64
	Wait         func(context.Context, time.Duration) error
}

func (policy ReconnectPolicy) Reconnect(ctx context.Context, acknowledged model.Revision, connect func(context.Context, model.Revision) error) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if connect == nil {
		return fmt.Errorf("reconnect callback is nil")
	}
	policy = policy.withDefaults()
	var lastErr error
	for attempt := 0; attempt < policy.MaxAttempts; attempt++ {
		if err := ctx.Err(); err != nil {
			return err
		}
		lastErr = connect(ctx, acknowledged)
		if lastErr == nil {
			return nil
		}
		var permanent permanentReconnectError
		if errors.As(lastErr, &permanent) || errors.Is(lastErr, ErrAuthenticationDenied) || errors.Is(lastErr, ErrIncompatibleProtocol) {
			return lastErr
		}
		if attempt == policy.MaxAttempts-1 {
			break
		}
		if err := policy.Wait(ctx, policy.delay(attempt)); err != nil {
			return err
		}
	}
	return fmt.Errorf("collaboration reconnect exhausted %d attempts: %w", policy.MaxAttempts, lastErr)
}

func (policy ReconnectPolicy) withDefaults() ReconnectPolicy {
	if policy.InitialDelay <= 0 {
		policy.InitialDelay = 250 * time.Millisecond
	}
	if policy.MaxDelay <= 0 {
		policy.MaxDelay = 10 * time.Second
	}
	if policy.InitialDelay > policy.MaxDelay {
		policy.InitialDelay = policy.MaxDelay
	}
	if policy.Jitter < 0 {
		policy.Jitter = 0
	}
	if policy.Jitter > 1 {
		policy.Jitter = 1
	}
	if policy.MaxAttempts <= 0 {
		policy.MaxAttempts = 8
	}
	if policy.Random == nil {
		policy.Random = func() float64 { return 0.5 }
	}
	if policy.Wait == nil {
		policy.Wait = waitReconnectDelay
	}
	return policy
}

func (policy ReconnectPolicy) delay(attempt int) time.Duration {
	delay := policy.InitialDelay
	for index := 0; index < attempt && delay < policy.MaxDelay; index++ {
		if delay > policy.MaxDelay/2 {
			delay = policy.MaxDelay
			break
		}
		delay *= 2
	}
	random := policy.Random()
	if random < 0 {
		random = 0
	}
	if random > 1 {
		random = 1
	}
	factor := 1 + policy.Jitter*(2*random-1)
	delay = time.Duration(float64(delay) * factor)
	if delay > policy.MaxDelay {
		return policy.MaxDelay
	}
	return delay
}

func waitReconnectDelay(ctx context.Context, delay time.Duration) error {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-timer.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
