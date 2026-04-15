package rpc

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"time"
)

const (
	defaultRequestMaxBytes  = 1 << 20
	defaultResponseMaxBytes = 32 << 20
)

// DefaultResponseMaxBytes returns the client's current maximum response frame
// size so the broker can avoid sending oversized payloads.
func DefaultResponseMaxBytes() uint32 {
	return defaultResponseMaxBytes
}

// Call sends one request to a Unix socket and reads one response.
func Call(ctx context.Context, socketPath string, req Request) (Response, error) {
	dialer := net.Dialer{}
	conn, err := dialer.DialContext(ctx, "unix", socketPath)
	if err != nil {
		return Response{}, fmt.Errorf("dial socket: %w", err)
	}
	defer conn.Close()

	_ = conn.SetDeadline(time.Now().Add(15 * time.Second))

	payload, err := json.Marshal(req)
	if err != nil {
		return Response{}, fmt.Errorf("marshal request: %w", err)
	}
	if err := WriteFrame(conn, payload); err != nil {
		return Response{}, err
	}

	respPayload, err := ReadFrame(conn, defaultResponseMaxBytes)
	if err != nil {
		return Response{}, err
	}

	var resp Response
	if err := json.Unmarshal(respPayload, &resp); err != nil {
		return Response{}, fmt.Errorf("parse response: %w", err)
	}
	return resp, nil
}
