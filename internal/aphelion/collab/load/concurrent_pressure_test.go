package load_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"
	loadscenario "sdmm/internal/aphelion/collab/load"
	"sdmm/internal/aphelion/collab/protocol"
	"sdmm/internal/aphelion/collab/server"
)

// The proxy holds every operation response until all operations and at least
// one presence per client have been forwarded. Serial offering or presence
// after the acknowledgement loop cannot open the gate.
type concurrentProxy struct {
	service                          http.Handler
	upstream                         string
	mu                               sync.Mutex
	nextClient, operations, presence int
	wantOperations, wantPresence     int
	gate                             chan struct{}
	once                             sync.Once
	stallClient                      int
	corruptClient                    int
}

func (proxy *concurrentProxy) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/v1/collaboration" {
		proxy.service.ServeHTTP(w, r)
		return
	}
	front, err := websocket.Accept(w, r, &websocket.AcceptOptions{InsecureSkipVerify: true, Subprotocols: []string{"apheliondmm.collaboration.v1"}})
	if err != nil {
		return
	}
	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()
	// Forward end-to-end headers only: each hop negotiates its own WebSocket
	// handshake and extensions, as a real proxy does.
	header := http.Header{}
	for _, name := range []string{"Authorization", "Origin"} {
		if value := r.Header.Get(name); value != "" {
			header.Set(name, value)
		}
	}
	back, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(proxy.upstream, "http")+r.URL.Path, &websocket.DialOptions{HTTPHeader: header, Subprotocols: []string{"apheliondmm.collaboration.v1"}})
	if err != nil {
		_ = front.CloseNow()
		return
	}
	front.SetReadLimit(protocol.MaxMessageBytes)
	back.SetReadLimit(protocol.MaxMessageBytes)
	proxy.mu.Lock()
	client := proxy.nextClient
	proxy.nextClient++
	proxy.mu.Unlock()
	done := make(chan struct{}, 2)
	go func() {
		defer func() { done <- struct{}{} }()
		for {
			kind, data, err := front.Read(ctx)
			if err != nil {
				return
			}
			if err := back.Write(ctx, kind, data); err != nil {
				return
			}
			message, err := protocol.DecodeClient(data)
			if err != nil {
				return
			}
			proxy.mu.Lock()
			if message.Envelope.Type == protocol.ClientOperationSubmit {
				proxy.operations++
			}
			if message.Envelope.Type == protocol.ClientPresenceUpdate {
				proxy.presence++
			}
			if proxy.operations >= proxy.wantOperations && proxy.presence >= proxy.wantPresence {
				proxy.once.Do(func() { close(proxy.gate) })
			}
			proxy.mu.Unlock()
		}
	}()
	go func() {
		defer func() { done <- struct{}{} }()
		for {
			kind, data, err := back.Read(ctx)
			if err != nil {
				return
			}
			message, err := protocol.DecodeServer(data)
			if err != nil {
				return
			}
			if message.Envelope.Type == protocol.ServerOperationAccepted || message.Envelope.Type == protocol.ServerOperationRejected {
				select {
				case <-proxy.gate:
				case <-ctx.Done():
					return
				}
				if client == proxy.stallClient {
					<-ctx.Done()
					return
				}
				if client == proxy.corruptClient && message.Envelope.Type == protocol.ServerOperationAccepted {
					payload := message.Payload.(*protocol.OperationAcceptedPayload)
					payload.MapHash = strings.Repeat("f", 64)
					message.Envelope.Payload, _ = json.Marshal(payload)
					data, _ = json.Marshal(message.Envelope)
				}
			}
			if err := front.Write(ctx, kind, data); err != nil {
				return
			}
		}
	}()
	<-done
	cancel()
	_ = front.CloseNow()
	_ = back.CloseNow()
	<-done
}

func TestConcurrentRunnerDoesNotWaitForAcknowledgementToOfferWork(t *testing.T) {
	testConcurrentProxy(t, -1, -1)
}
func TestConcurrentRunnerReportsSlowConsumerAsFailure(t *testing.T) {
	testConcurrentProxy(t, 1, -1)
}
func TestConcurrentRunnerDetectsCorruptClientStream(t *testing.T) {
	testConcurrentProxy(t, -1, 1)
}

func testConcurrentProxy(t *testing.T, stall, corrupt int) {
	t.Helper()
	scenario, err := loadscenario.GenerateConcurrent(loadscenario.ConcurrentConfig{Config: loadscenario.Config{Seed: 9026, Clients: 3, Operations: 9, PresencePerClient: 2, MaxX: 3, MaxY: 3}, ConflictPairs: 2})
	if err != nil {
		t.Fatal(err)
	}
	service := server.NewService(server.ServiceConfig{AllowedOrigins: []string{"http://127.0.0.1"}})
	t.Cleanup(func() { _ = service.Shutdown(context.Background()) })
	backend := httptest.NewServer(service.Handler())
	t.Cleanup(backend.Close)
	proxy := &concurrentProxy{service: service.Handler(), upstream: backend.URL, wantOperations: 9, wantPresence: 3, gate: make(chan struct{}), stallClient: stall, corruptClient: corrupt}
	front := httptest.NewServer(proxy)
	t.Cleanup(front.Close)
	config := createConcurrentSession(t, scenario, service, front.URL)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	result, err := loadscenario.RunConcurrent(ctx, config, scenario)
	if stall < 0 && corrupt < 0 {
		if err != nil || !result.GatePassed {
			t.Fatalf("concurrent offer failed: %v result=%#v", err, result)
		}
		select {
		case <-proxy.gate:
		default:
			t.Fatal("runner passed without concurrent operations and presence")
		}
	} else {
		if err == nil || result.GatePassed || result.Failure == "" {
			t.Fatalf("fault incorrectly passed: err=%v result=%#v", err, result)
		}
		if result.SentOperations != 9 || result.AppliedDeliveries >= scenario.ExpectedAccepted*3 {
			t.Fatalf("partial failure counts hide missing client application: %#v", result)
		}
		if stall >= 0 && !result.TimedOut {
			t.Fatal("slow-reader deadline was not reported")
		}
	}
}
