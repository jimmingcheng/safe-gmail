package rpc

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"strings"
)

// RequestValidationError carries the stable protocol error code for envelope validation.
type RequestValidationError struct {
	Code    string
	Message string
}

func (e *RequestValidationError) Error() string {
	return e.Message
}

// ValidateRequest performs envelope-level validation.
func ValidateRequest(req Request) error {
	if req.V != Version1 {
		return &RequestValidationError{
			Code:    "unsupported_version",
			Message: fmt.Sprintf("unsupported version: %d", req.V),
		}
	}
	if strings.TrimSpace(req.ID) == "" {
		return &RequestValidationError{Code: "invalid_request", Message: "missing id"}
	}
	if len(req.ID) > 64 {
		return &RequestValidationError{Code: "invalid_request", Message: "id too long"}
	}
	if strings.TrimSpace(req.Method) == "" {
		return &RequestValidationError{Code: "invalid_request", Message: "missing method"}
	}
	if len(req.Params) == 0 {
		return &RequestValidationError{Code: "invalid_request", Message: "missing params"}
	}
	if !json.Valid(req.Params) {
		return &RequestValidationError{Code: "invalid_request", Message: "params must be valid JSON"}
	}
	trimmed := bytes.TrimSpace(req.Params)
	if len(trimmed) == 0 || trimmed[0] != '{' {
		return &RequestValidationError{Code: "invalid_request", Message: "params must be a JSON object"}
	}
	return nil
}

// DecodeParams decodes params into a typed payload and rejects unknown fields.
func DecodeParams(raw json.RawMessage, dst any) error {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		return err
	}
	if err := dec.Decode(&struct{}{}); err != io.EOF {
		return fmt.Errorf("params must contain exactly one JSON object")
	}
	return nil
}
