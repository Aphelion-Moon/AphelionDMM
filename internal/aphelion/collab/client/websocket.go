package client

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/coder/websocket"

	"sdmm/internal/aphelion/collab/bulktransport"
	"sdmm/internal/aphelion/collab/model"
	"sdmm/internal/aphelion/collab/protocol"
	"sdmm/internal/aphelion/collab/transaction"
)

const collaborationSubprotocol = "apheliondmm.collaboration.v1"
const transportKeepaliveNonce = "aphelion-transport-keepalive"

var ErrTransportNotConnected = errors.New("collaboration transport is not connected")

type TransportConfig struct {
	BulkSpoolBytes     int64
	BulkWorkingBytes   int64
	BulkSpoolDirectory string
	DurableQueueSize   int
	DialTimeout        time.Duration
	WriteTimeout       time.Duration
	KeepaliveInterval  time.Duration
}

type WebSocketTransport struct {
	config      TransportConfig
	bulk        *bulktransport.Codec
	bulkEnabled bool

	mutex       sync.RWMutex
	connection  *websocket.Conn
	cancel      context.CancelFunc
	durable     chan protocol.ClientEnvelope
	bulkWrites  chan bulkWrite
	presence    chan protocol.ClientEnvelope
	done        chan struct{}
	terminalErr error
	presenceMu  sync.Mutex
	errorOnce   sync.Once
}

type bulkWrite struct {
	context context.Context
	body    *transaction.Body
	result  chan error
}

func NewWebSocketTransport(config TransportConfig) *WebSocketTransport {
	if config.DurableQueueSize <= 0 {
		config.DurableQueueSize = 128
	}
	if config.DialTimeout <= 0 {
		config.DialTimeout = 5 * time.Second
	}
	if config.WriteTimeout <= 0 {
		config.WriteTimeout = 5 * time.Second
	}
	if config.KeepaliveInterval <= 0 {
		config.KeepaliveInterval = 3 * time.Second
	}
	if config.BulkSpoolBytes <= 0 {
		config.BulkSpoolBytes = bulktransport.DefaultSpoolBytes
	}
	if config.BulkWorkingBytes <= 0 {
		config.BulkWorkingBytes = bulktransport.DefaultWorkingBytes
	}
	return &WebSocketTransport{config: config, bulk: bulktransport.NewWithWorkingBudget(config.BulkSpoolDirectory, config.BulkSpoolBytes, config.BulkWorkingBytes)}
}

func (transport *WebSocketTransport) Connect(ctx context.Context, request protocol.JoinRequest, receive func(protocol.ServerEnvelope)) error {
	if receive == nil {
		return fmt.Errorf("receive callback is nil")
	}
	websocketURL, err := collaborationURL(request.BaseURL)
	if err != nil {
		return err
	}
	if request.Token == "" || request.SessionID == "" || request.Origin == "" {
		return fmt.Errorf("join token, session id, and origin are required")
	}

	transport.mutex.Lock()
	if transport.connection != nil {
		transport.mutex.Unlock()
		return fmt.Errorf("transport is already connected")
	}
	transport.mutex.Unlock()
	dialContext, cancelDial := context.WithTimeout(ctx, transport.config.DialTimeout)
	connection, response, err := websocket.Dial(dialContext, websocketURL, &websocket.DialOptions{
		HTTPHeader:   http.Header{"Authorization": []string{"Bearer " + request.Token}, "Origin": []string{request.Origin}},
		Subprotocols: []string{protocol.BulkSubprotocol, collaborationSubprotocol},
	})
	cancelDial()
	if err != nil {
		if response != nil {
			_ = response.Body.Close()
			switch response.StatusCode {
			case http.StatusUnauthorized, http.StatusForbidden:
				return fmt.Errorf("%w: HTTP %d", ErrAuthenticationDenied, response.StatusCode)
			case http.StatusUpgradeRequired, http.StatusBadRequest:
				return fmt.Errorf("%w: HTTP %d", ErrIncompatibleProtocol, response.StatusCode)
			}
		}
		return fmt.Errorf("connect collaboration WebSocket: %w", err)
	}
	connection.SetReadLimit(protocol.MaxMessageBytes)
	connectionContext, cancelConnection := context.WithCancel(context.WithoutCancel(ctx))
	transport.mutex.Lock()
	transport.connection = connection
	transport.cancel = cancelConnection
	transport.durable = make(chan protocol.ClientEnvelope, transport.config.DurableQueueSize)
	transport.bulkWrites = make(chan bulkWrite)
	transport.presence = make(chan protocol.ClientEnvelope, 1)
	transport.done = make(chan struct{})
	transport.mutex.Unlock()

	joinPayload, err := json.Marshal(protocol.JoinPayload{JoinToken: request.Token, AcknowledgedRevision: request.AcknowledgedRevision})
	if err != nil {
		cancelConnection()
		_ = connection.CloseNow()
		return err
	}
	join := protocol.ClientEnvelope{ProtocolVersion: model.ProtocolVersion, MessageID: "join", SessionID: request.SessionID, Type: protocol.ClientJoin, Payload: joinPayload}
	if err := transport.write(connectionContext, connection, join); err != nil {
		cancelConnection()
		_ = connection.CloseNow()
		return fmt.Errorf("send collaboration join: %w", err)
	}

	var workers sync.WaitGroup
	workers.Add(2)
	go func() {
		defer workers.Done()
		transport.readLoop(connectionContext, connection, receive)
	}()
	go func() {
		defer workers.Done()
		transport.writeLoop(connectionContext, connection, request.SessionID)
	}()
	go func() {
		workers.Wait()
		_ = connection.CloseNow()
		transport.mutex.RLock()
		done := transport.done
		transport.mutex.RUnlock()
		close(done)
	}()
	return nil
}

func (transport *WebSocketTransport) Send(ctx context.Context, message protocol.ClientEnvelope) error {
	transport.mutex.RLock()
	message.BulkSession = transport.bulkEnabled
	transport.mutex.RUnlock()
	if _, err := protocol.DecodeClientEnvelope(message); err != nil {
		return err
	}
	if message.BulkOperation != nil {
		return transport.SendOperation(ctx, message.SessionID, *message.BulkOperation)
	}
	transport.mutex.RLock()
	durable, presence, done := transport.durable, transport.presence, transport.done
	transport.mutex.RUnlock()
	if durable == nil || presence == nil || done == nil {
		return ErrTransportNotConnected
	}
	if message.Type == protocol.ClientPresenceUpdate {
		transport.presenceMu.Lock()
		defer transport.presenceMu.Unlock()
		select {
		case presence <- message:
			return nil
		default:
			select {
			case <-presence:
			default:
			}
			select {
			case presence <- message:
				return nil
			case <-done:
				return transport.result()
			case <-ctx.Done():
				return ctx.Err()
			}
		}
	}
	select {
	case durable <- message:
		return nil
	case <-done:
		return transport.result()
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (transport *WebSocketTransport) Close(status websocket.StatusCode, reason string) error {
	transport.mutex.RLock()
	connection, cancel := transport.connection, transport.cancel
	transport.mutex.RUnlock()
	if connection == nil || cancel == nil {
		return ErrTransportNotConnected
	}
	err := connection.Close(status, reason)
	cancel()
	return err
}

func (transport *WebSocketTransport) Wait(ctx context.Context) error {
	transport.mutex.RLock()
	done := transport.done
	transport.mutex.RUnlock()
	if done == nil {
		return ErrTransportNotConnected
	}
	select {
	case <-done:
		return transport.result()
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (transport *WebSocketTransport) readLoop(ctx context.Context, connection *websocket.Conn, receive func(protocol.ServerEnvelope)) {
	for {
		kind, data, err := connection.Read(ctx)
		if err != nil {
			transport.finish(err)
			return
		}
		var decoded protocol.DecodedServer
		release := func() {}
		if kind == websocket.MessageBinary {
			transport.mutex.RLock()
			enabled := transport.bulkEnabled
			transport.mutex.RUnlock()
			if !enabled {
				transport.finish(fmt.Errorf("unnegotiated bulk acceptance"))
				return
			}
			h, op, lease, readErr := transport.bulk.ReadAdmitted(ctx, connection, data, transport.config.WriteTimeout)
			if lease != nil {
				release = lease
			}
			err = readErr
			if err == nil {
				decoded, err = bulktransport.Acceptance(h, op)
			}
		} else {
			transport.mutex.RLock()
			enabled := transport.bulkEnabled
			transport.mutex.RUnlock()
			decoded, err = protocol.DecodeServerForSession(data, enabled)
		}
		if err != nil {
			release()
			transport.finish(fmt.Errorf("decode server envelope: %w", err))
			return
		}
		if joined, ok := decoded.Payload.(*protocol.JoinedPayload); ok {
			if joined.BulkEdits && connection.Subprotocol() != protocol.BulkSubprotocol {
				transport.finish(ErrIncompatibleProtocol)
				return
			}
			transport.mutex.Lock()
			transport.bulkEnabled = joined.BulkEdits
			transport.mutex.Unlock()
		}
		if pong, ok := decoded.Payload.(*protocol.PongPayload); !ok || decoded.Envelope.Type != protocol.ServerPong || pong.Nonce != transportKeepaliveNonce {
			receive(decoded.Envelope)
		}
		release()
	}
}

func (transport *WebSocketTransport) writeLoop(ctx context.Context, connection *websocket.Conn, sessionID string) {
	keepalive := time.NewTicker(transport.config.KeepaliveInterval)
	defer keepalive.Stop()
	payload, _ := json.Marshal(protocol.PingPayload{Nonce: transportKeepaliveNonce})
	heartbeat := protocol.ClientEnvelope{ProtocolVersion: model.ProtocolVersion, MessageID: "transport-keepalive", SessionID: sessionID, Type: protocol.ClientPing, Payload: payload}
	for {
		select {
		case <-ctx.Done():
			transport.finish(ctx.Err())
			return
		case request := <-transport.bulkWrites:
			// Whole bodies stay contiguous; a heartbeat never splits their frames.
			writeCtx, cancel := context.WithCancel(request.context)
			stop := context.AfterFunc(ctx, cancel)
			if ctx.Err() != nil {
				cancel()
			}
			err := transport.bulk.WriteBody(writeCtx, connection, request.body, transport.config.WriteTimeout)
			stop()
			cancel()
			_ = request.body.Close()
			request.result <- err
			if err != nil {
				transport.finish(err)
				return
			}
		case message := <-transport.durable:
			if err := transport.write(ctx, connection, message); err != nil {
				transport.finish(err)
				return
			}
		case message := <-transport.presence:
			if err := transport.write(ctx, connection, message); err != nil {
				transport.finish(err)
				return
			}
		case <-keepalive.C:
			if err := transport.write(ctx, connection, heartbeat); err != nil {
				transport.finish(err)
				return
			}
		}
	}
}

func (transport *WebSocketTransport) write(ctx context.Context, connection *websocket.Conn, message protocol.ClientEnvelope) error {
	if message.BulkOperation != nil {
		return transport.bulk.Write(ctx, connection, bulktransport.SubmissionHeader(message), message.BulkOperation.Changes, transport.config.WriteTimeout)
	}
	data, err := json.Marshal(message)
	if err != nil {
		return err
	}
	writeContext, cancel := context.WithTimeout(ctx, transport.config.WriteTimeout)
	defer cancel()
	return connection.Write(writeContext, websocket.MessageText, data)
}

// SendOperation preserves the legacy wire contract unless the joined session
// explicitly requires bulk-edit-v2. Queue ownership is detached from the caller.
func (transport *WebSocketTransport) SendOperation(ctx context.Context, sessionID string, operation model.Operation) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	transport.mutex.RLock()
	enabled := transport.bulkEnabled
	writes, done := transport.bulkWrites, transport.done
	transport.mutex.RUnlock()
	envelope := protocol.ClientEnvelope{ProtocolVersion: model.ProtocolVersion, MessageID: string(operation.OperationID), SessionID: sessionID, Type: protocol.ClientOperationSubmit}
	if !enabled || len(operation.Changes) <= 64 {
		data, err := json.Marshal(protocol.OperationSubmitPayload{Operation: operation})
		if err != nil {
			return err
		}
		envelope.Payload = data
		if !enabled || len(data)+1024 <= protocol.MaxMessageBytes {
			return transport.Send(ctx, envelope)
		}
	}
	if writes == nil || done == nil {
		return ErrTransportNotConnected
	}
	envelope.Payload = nil
	envelope.BulkOperation = &operation
	if _, err := protocol.DecodeClientEnvelope(envelope); err != nil {
		return err
	}
	body, err := transport.bulk.Prepare(ctx, bulktransport.SubmissionHeader(envelope), operation.Changes)
	if err != nil {
		return err
	}
	request := bulkWrite{context: ctx, body: body, result: make(chan error, 1)}
	select {
	case writes <- request: // Writer now owns body cleanup, including cancellation.
	case <-ctx.Done():
		_ = body.Close()
		return ctx.Err()
	case <-done:
		_ = body.Close()
		return transport.result()
	}
	select {
	case err := <-request.result:
		return err
	case <-ctx.Done():
		return ctx.Err()
	case <-done:
		return transport.result()
	}
}

func (transport *WebSocketTransport) finish(err error) {
	transport.errorOnce.Do(func() {
		transport.mutex.Lock()
		transport.terminalErr = err
		cancel := transport.cancel
		transport.mutex.Unlock()
		if cancel != nil {
			cancel()
		}
	})
}

func (transport *WebSocketTransport) result() error {
	transport.mutex.RLock()
	defer transport.mutex.RUnlock()
	return transport.terminalErr
}

func collaborationURL(baseURL string) (string, error) {
	parsed, err := url.Parse(baseURL)
	if err != nil {
		return "", fmt.Errorf("parse collaboration base URL: %w", err)
	}
	if parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return "", fmt.Errorf("collaboration base URL cannot contain credentials, query, or fragment")
	}
	switch strings.ToLower(parsed.Scheme) {
	case "http":
		ip := net.ParseIP(parsed.Hostname())
		if ip == nil || !ip.IsLoopback() {
			return "", fmt.Errorf("cleartext collaboration base URL is restricted to loopback IP addresses")
		}
		parsed.Scheme = "ws"
	case "https":
		parsed.Scheme = "wss"
	default:
		return "", fmt.Errorf("collaboration base URL scheme must be http or https")
	}
	parsed.Path = "/v1/collaboration"
	return parsed.String(), nil
}
