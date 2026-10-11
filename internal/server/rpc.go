package server

import (
	"encoding/json"
	"fmt"
)

const (
	Version            = "2.0"
	CodeParse          = -32700
	CodeInvalidReq     = -32600
	CodeMethodNotFound = -32601
	CodeInvalidParams  = -32602
	CodeInternal       = -32603
)

type Request struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type Response struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  any             `json:"result,omitempty"`
	Error   *RPCError       `json:"error,omitempty"`
}

type RPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data,omitempty"`
}

func (e *RPCError) Error() string { return fmt.Sprintf("rpc %d: %s", e.Code, e.Message) }

func EncodeRequest(id json.RawMessage, method string, params any) ([]byte, error) {
	r := Request{JSONRPC: Version, ID: id, Method: method}
	if params != nil {
		b, err := json.Marshal(params)
		if err != nil {
			return nil, err
		}
		r.Params = b
	}
	b, err := json.Marshal(r)
	if err != nil {
		return nil, err
	}
	return append(b, '\n'), nil
}

func EncodeResponse(id json.RawMessage, result any, rpcErr *RPCError) ([]byte, error) {
	if result == nil && rpcErr == nil {
		result = map[string]any{}
	}
	b, err := json.Marshal(Response{JSONRPC: Version, ID: id, Result: result, Error: rpcErr})
	if err != nil {
		return nil, err
	}
	return append(b, '\n'), nil
}

func Errf(code int, format string, args ...any) *RPCError {
	return &RPCError{Code: code, Message: fmt.Sprintf(format, args...)}
}
