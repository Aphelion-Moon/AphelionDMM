package server

import (
	"fmt"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/coder/websocket"
)

func NewTrustedProxyHandler(next http.Handler, trustedCIDRs []string) (http.Handler, error) {
	if next == nil {
		return nil, fmt.Errorf("trusted proxy handler is nil")
	}
	trusted := make([]*net.IPNet, 0, len(trustedCIDRs))
	for _, value := range trustedCIDRs {
		_, network, err := net.ParseCIDR(value)
		if err != nil {
			return nil, fmt.Errorf("parse trusted proxy CIDR %q: %w", value, err)
		}
		trusted = append(trusted, network)
	}
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if forwarded := forwardedClientIP(request, trusted); forwarded != nil {
			request.RemoteAddr = net.JoinHostPort(forwarded.String(), "0")
		}
		next.ServeHTTP(writer, request)
	}), nil
}

func forwardedClientIP(request *http.Request, trusted []*net.IPNet) net.IP {
	peer := net.ParseIP(remoteIP(request))
	if peer == nil || !containsIP(trusted, peer) {
		return nil
	}
	values := strings.Split(request.Header.Get("X-Forwarded-For"), ",")
	forwarded := make([]net.IP, 0, len(values))
	for _, value := range values {
		ip := net.ParseIP(strings.TrimSpace(value))
		if ip == nil {
			return nil
		}
		forwarded = append(forwarded, ip)
	}
	for index := len(forwarded) - 1; index >= 0; index-- {
		if !containsIP(trusted, forwarded[index]) {
			return forwarded[index]
		}
	}
	if len(forwarded) > 0 {
		return forwarded[0]
	}
	return nil
}

func containsIP(networks []*net.IPNet, ip net.IP) bool {
	for index := range networks {
		if networks[index].Contains(ip) {
			return true
		}
	}
	return false
}

const (
	CloseSlowConsumer websocket.StatusCode = 4408
	CloseRateLimited  websocket.StatusCode = 4429
)

type rateEntry struct {
	windowStarted time.Time
	lastSeen      time.Time
	count         int
}

type rateLimiter struct {
	mutex       sync.Mutex
	policy      RateLimit
	maxEntries  int
	entries     map[string]rateEntry
	lastCleanup time.Time
}

func newRateLimiter(policy RateLimit, maxEntries int) *rateLimiter {
	return &rateLimiter{policy: policy, maxEntries: maxEntries, entries: make(map[string]rateEntry)}
}

func (limiter *rateLimiter) Allow(key string, now time.Time) bool {
	limiter.mutex.Lock()
	defer limiter.mutex.Unlock()
	limiter.cleanup(now)
	entry, exists := limiter.entries[key]
	if !exists && len(limiter.entries) >= limiter.maxEntries {
		return false
	}
	if !exists || now.Sub(entry.windowStarted) >= limiter.policy.Window {
		limiter.entries[key] = rateEntry{windowStarted: now, lastSeen: now, count: 1}
		return true
	}
	entry.lastSeen = now
	entry.count++
	limiter.entries[key] = entry
	return entry.count <= limiter.policy.Burst
}

func (limiter *rateLimiter) cleanup(now time.Time) {
	if !limiter.lastCleanup.IsZero() && now.Sub(limiter.lastCleanup) < limiter.policy.Window {
		return
	}
	for key, entry := range limiter.entries {
		if now.Sub(entry.lastSeen) >= limiter.policy.Window {
			delete(limiter.entries, key)
		}
	}
	limiter.lastCleanup = now
}

func remoteIP(request *http.Request) string {
	host, _, err := net.SplitHostPort(request.RemoteAddr)
	if err == nil {
		return host
	}
	return request.RemoteAddr
}

// presenceGate limits one connection's presence stream. Presence is lossy, so
// excess messages are dropped, not rejected. Only several consecutive windows
// over the limit count as abuse, which tolerates a stalled read loop draining
// a backlog at wire speed. A gate is owned by one connection loop and is not
// safe for concurrent use.
type presenceGate struct {
	policy        RateLimit
	abuseWindows  int
	windowStarted time.Time
	count         int
	consecutive   int
	windowCounted bool
	started       bool
}

func newPresenceGate(policy RateLimit, abuseWindows int) *presenceGate {
	if abuseWindows <= 0 {
		abuseWindows = defaultPresenceAbuseWindows
	}
	return &presenceGate{policy: policy, abuseWindows: abuseWindows}
}

// Allow reports whether the message should be applied and whether the
// connection has now exceeded the limit for too many consecutive windows.
func (gate *presenceGate) Allow(now time.Time) (allowed bool, abuse bool) {
	if !gate.started || now.Sub(gate.windowStarted) >= gate.policy.Window {
		// An idle window between bursts breaks the consecutive run.
		if !gate.started || !gate.windowCounted || now.Sub(gate.windowStarted) >= 2*gate.policy.Window {
			gate.consecutive = 0
		}
		gate.started = true
		gate.windowStarted = now
		gate.count = 0
		gate.windowCounted = false
	}
	gate.count++
	if gate.count <= gate.policy.Burst {
		return true, false
	}
	if !gate.windowCounted {
		gate.windowCounted = true
		gate.consecutive++
	}
	return false, gate.consecutive >= gate.abuseWindows
}
