package jsonrpc

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"reflect"
	"strings"
	"sync"
)

// JSON-RPC 2.0 server for AI Chain node interaction.

type Request struct {
	JSONRPC string          `json:"jsonrpc"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
	ID      interface{}     `json:"id"`
}

type Response struct {
	JSONRPC string      `json:"jsonrpc"`
	Result  interface{} `json:"result,omitempty"`
	Error   *RPCError   `json:"error,omitempty"`
	ID      interface{} `json:"id"`
}

type RPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// Standard JSON-RPC error codes.
const (
	ErrParse     = -32700
	ErrInvalid   = -32600
	ErrMethod    = -32601
	ErrParams    = -32602
	ErrInternal  = -32603
)

// Server is the JSON-RPC HTTP server.
type Server struct {
	mu       sync.RWMutex
	methods  map[string]MethodHandler
	httpSrv  *http.Server
}

// MethodHandler is a function that handles an RPC method call.
type MethodHandler func(params json.RawMessage) (interface{}, error)

// NewServer creates a JSON-RPC server.
func NewServer(addr string) *Server {
	s := &Server{
		methods: make(map[string]MethodHandler),
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/", s.handleHTTP)
	mux.HandleFunc("/health", s.handleHealth)

	s.httpSrv = &http.Server{
		Addr:    addr,
		Handler: mux,
	}

	return s
}

// RegisterMethod registers an RPC method.
func (s *Server) RegisterMethod(name string, handler MethodHandler) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.methods[name] = handler
}

// Start begins listening for HTTP requests.
func (s *Server) Start() error {
	log.Printf("[JSON-RPC] Listening on %s", s.httpSrv.Addr)
	return s.httpSrv.ListenAndServe()
}

// Stop gracefully shuts down the server.
func (s *Server) Stop() error {
	return s.httpSrv.Close()
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(200)
	w.Write([]byte(`{"status":"ok"}`))
}

func (s *Server) handleHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "POST required", 405)
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		s.writeError(w, nil, ErrParse, "failed to read body")
		return
	}
	defer r.Body.Close()

	// Batch request?
	if len(body) > 0 && body[0] == '[' {
		var requests []Request
		if err := json.Unmarshal(body, &requests); err != nil {
			s.writeError(w, nil, ErrParse, "invalid batch request")
			return
		}
		responses := make([]Response, len(requests))
		for i, req := range requests {
			responses[i] = s.handleRequest(&req)
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(responses)
		return
	}

	var req Request
	if err := json.Unmarshal(body, &req); err != nil {
		s.writeError(w, nil, ErrParse, "invalid request")
		return
	}

	resp := s.handleRequest(&req)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

func (s *Server) handleRequest(req *Request) Response {
	if req.JSONRPC != "2.0" {
		return s.errorResponse(req.ID, ErrInvalid, "jsonrpc must be 2.0")
	}

	s.mu.RLock()
	handler, exists := s.methods[req.Method]
	s.mu.RUnlock()

	if !exists {
		return s.errorResponse(req.ID, ErrMethod, fmt.Sprintf("method %s not found", req.Method))
	}

	result, err := handler(req.Params)
	if err != nil {
		return s.errorResponse(req.ID, ErrInternal, err.Error())
	}

	return Response{
		JSONRPC: "2.0",
		Result:  result,
		ID:      req.ID,
	}
}

func (s *Server) writeError(w http.ResponseWriter, id interface{}, code int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(s.errorResponse(id, code, msg))
}

func (s *Server) errorResponse(id interface{}, code int, msg string) Response {
	return Response{
		JSONRPC: "2.0",
		Error:   &RPCError{Code: code, Message: msg},
		ID:      id,
	}
}

// Ensure imports used.
var _ = strings.ToUpper
var _ = reflect.TypeOf
var _ = sync.Mutex{}
