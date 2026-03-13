package rpc

import (
	"errors"
	"testing"
)

func TestValidateRequestReturnsUnsupportedVersionCode(t *testing.T) {
	t.Parallel()

	err := ValidateRequest(Request{
		V:      2,
		ID:     "req-1",
		Method: MethodSystemPing,
		Params: []byte(`{}`),
	})
	if err == nil {
		t.Fatal("ValidateRequest() error = nil, want error")
	}

	var validationErr *RequestValidationError
	if !errors.As(err, &validationErr) {
		t.Fatalf("error type = %T, want *RequestValidationError", err)
	}
	if validationErr.Code != "unsupported_version" {
		t.Fatalf("validationErr.Code = %q, want unsupported_version", validationErr.Code)
	}
}

func TestDecodeParamsRejectsUnknownFields(t *testing.T) {
	t.Parallel()

	var params GmailGetMessageParams
	err := DecodeParams([]byte(`{"message_id":"msg-1","extra":true}`), &params)
	if err == nil {
		t.Fatal("DecodeParams() error = nil, want error")
	}
}
