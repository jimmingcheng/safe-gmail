package rpc

import (
	"encoding/binary"
	"fmt"
	"io"
)

// ReadFrame reads a single length-prefixed JSON frame.
func ReadFrame(r io.Reader, maxBytes uint32) ([]byte, error) {
	var header [4]byte
	if _, err := io.ReadFull(r, header[:]); err != nil {
		return nil, fmt.Errorf("read frame header: %w", err)
	}

	size := binary.BigEndian.Uint32(header[:])
	if size == 0 {
		return nil, fmt.Errorf("read frame: empty payload")
	}
	if maxBytes > 0 && size > maxBytes {
		return nil, fmt.Errorf("read frame: payload %d exceeds limit %d", size, maxBytes)
	}

	payload := make([]byte, size)
	if _, err := io.ReadFull(r, payload); err != nil {
		return nil, fmt.Errorf("read frame payload: %w", err)
	}
	return payload, nil
}

// WriteFrame writes a single length-prefixed JSON frame.
func WriteFrame(w io.Writer, payload []byte) error {
	if len(payload) == 0 {
		return fmt.Errorf("write frame: empty payload")
	}
	if len(payload) > int(^uint32(0)) {
		return fmt.Errorf("write frame: payload too large")
	}

	var header [4]byte
	binary.BigEndian.PutUint32(header[:], uint32(len(payload)))
	if _, err := w.Write(header[:]); err != nil {
		return fmt.Errorf("write frame header: %w", err)
	}
	if _, err := w.Write(payload); err != nil {
		return fmt.Errorf("write frame payload: %w", err)
	}
	return nil
}
