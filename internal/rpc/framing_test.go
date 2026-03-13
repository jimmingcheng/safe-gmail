package rpc

import (
	"bytes"
	"testing"
)

func TestFrameRoundTrip(t *testing.T) {
	var buf bytes.Buffer
	original := []byte(`{"hello":"world"}`)
	if err := WriteFrame(&buf, original); err != nil {
		t.Fatalf("WriteFrame: %v", err)
	}

	decoded, err := ReadFrame(&buf, 1024)
	if err != nil {
		t.Fatalf("ReadFrame: %v", err)
	}
	if string(decoded) != string(original) {
		t.Fatalf("round trip mismatch: got %q want %q", decoded, original)
	}
}
