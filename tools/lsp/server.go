// Copyright 2026 The OpenChoreo Authors
// SPDX-License-Identifier: Apache-2.0

package lsp

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/openchoreo/openchoreo/tools/lint/indexer"
	"github.com/openchoreo/openchoreo/tools/lsp/code-action"
	"github.com/openchoreo/openchoreo/tools/lsp/code-completion"
	"github.com/openchoreo/openchoreo/tools/lsp/code-definition"
	"github.com/openchoreo/openchoreo/tools/lsp/hover"
	"github.com/sourcegraph/jsonrpc2"
	"github.com/tliron/glsp"
	protocol "github.com/tliron/glsp/protocol_3_16"
	"github.com/tliron/glsp/server"
)

type Server struct {
	server    *server.Server
	documents map[string]*Document
	docMu     sync.RWMutex
	indexer   *indexer.Indexer
	// workspaceRoots are the normalized, non-overlapping working directories
	// this server indexes and watches (from initialize workspaceFolders or,
	// for legacy single-folder clients, rootUri).
	workspaceRoots []string
	logger         *slog.Logger

	connMu sync.RWMutex
	conn   *jsonrpc2.Conn

	// Workspace scanning runs once per session in a background goroutine so
	// initialize returns promptly on large workspaces.
	indexOnce sync.Once
	// indexDone is closed once the initial workspace scan finishes, whether it
	// succeeded or failed. Callers (e.g. tests) block on it to observe the scan.
	indexDone chan struct{}
	// indexProgress reports $/progress for the scan when the client declared
	// Window.WorkDoneProgress support.
	indexProgress bool

	// Debounced diagnostics state: a single pending publish per server, reset
	// on every edit so bursts of keystrokes collapse into one revalidation.
	diagMu      sync.Mutex
	diagTimer   *time.Timer
	diagURI     string
	diagVersion int
	diagText    string
	diagPending bool
	diagGen     int

	// After a client edit re-files one document in the index, other open
	// documents may reference different resources (e.g. a metadata.name
	// change). These fields coalesce the follow-up revalidation of every open
	// document except the one that changed, so bursts collapse into one pass.
	revalTimer *time.Timer
	revalGen   int
	revalURI   string

	// Filesystem watching keeps the index fresh for on-disk changes to files
	// that are not open in any client (edits in other tools, git operations).
	watchMu      sync.Mutex
	watcher      *fsnotify.Watcher
	watchRoots   []string
	watchPending map[string]*time.Timer
	watchClosed  bool

	// Validation cache: per-URI cached parse + rule-engine results. The
	// reference diagnostics are keyed to the indexer version so a disk change
	// re-runs only the reference checks. cacheHits/cacheMisses are exposed for
	// tests and future telemetry.
	cacheMu     sync.Mutex
	cache       map[string]*validationEntry
	cacheHits   uint64
	cacheMisses uint64
}

// New creates a new OpenChoreo language server.
func NewServer() *Server {
	handler := protocol.Handler{}

	s := &Server{
		server:    server.NewServer(&handler, "OpenChoreo Language Server", false),
		documents: make(map[string]*Document),
		indexer:   indexer.NewIndexer(),
		indexDone: make(chan struct{}),
		cache:     make(map[string]*validationEntry),
		logger:    slog.New(slog.NewTextHandler(os.Stderr, nil)),
	}
	handler.Initialize = WithRequestRecovery(s, "initialize", s.Initialize)
	handler.Initialized = WithHandlerRecovery(s, "initialized", s.Initialized)
	handler.SetTrace = WithHandlerRecovery(s, "setTrace", s.SetTrace)
	handler.WorkspaceDidChangeConfiguration = WithHandlerRecovery(s, "didChangeConfiguration", s.DidChangeConfiguration)
	// VSCode routinely sends $/cancelRequest notifications (e.g. while typing
	// quickly); accept them silently instead of logging "method not supported".
	handler.CancelRequest = WithHandlerRecovery(s, "cancelRequest", func(context *glsp.Context, params *protocol.CancelParams) error {
		return nil
	})
	handler.TextDocumentDidOpen = WithHandlerRecovery(s, "didOpen", s.DidOpen)
	handler.TextDocumentDidChange = WithHandlerRecovery(s, "didChange", s.DidChange)
	handler.TextDocumentDidClose = WithHandlerRecovery(s, "didClose", s.DidClose)
	handler.TextDocumentCompletion = WithRequestRecovery(s, "completion", s.Completion)
	handler.TextDocumentHover = WithRequestRecovery(s, "hover", s.Hover)
	handler.TextDocumentDefinition = WithRequestRecovery(s, "definition", s.Definition)
	handler.TextDocumentCodeAction = WithRequestRecovery(s, "codeAction", s.CodeAction)

	return s
}

// SetLogger replaces the server's default stderr logger.
func (s *Server) SetLogger(logger *slog.Logger) {
	if logger != nil {
		s.logger = logger
	}
}

func (s *Server) Initialized(context *glsp.Context, params *protocol.InitializedParams) error {
	s.notifyShow(context, protocol.MessageTypeInfo, "OpenChoreo language server initialized.")
	return nil
}

// SetTrace stores the client's trace setting so window/logMessage notifications
// are gated by the requested verbosity.
func (s *Server) SetTrace(context *glsp.Context, params *protocol.SetTraceParams) error {
	protocol.SetTraceValue(params.Value)
	if s.logger != nil {
		s.logger.Info("trace setting updated", "value", params.Value)
	}
	s.notifyLog(context, protocol.MessageTypeInfo, "Trace level set to "+string(params.Value))
	return nil
}

// Start runs the server over stdio, retaining the underlying connection so
// notifications can be pushed to the client asynchronously (debounced
// diagnostics, $/progress) after a handler has returned.
func (s *Server) Start() error {
	s.SetConnection(s.server.GetStdio())

	conn := s.Connection()
	<-conn.DisconnectNotify()

	s.SetConnection(nil)
	s.StopWatcher()
	return nil
}

// SetConnection attaches the outgoing JSON-RPC connection used for
// asynchronous notifications. It is called by Start and can be reused to
// serve over other transports.
func (s *Server) SetConnection(conn *jsonrpc2.Conn) {
	s.connMu.Lock()
	defer s.connMu.Unlock()
	s.conn = conn
}

// Connection returns the currently attached connection, or nil before Start.
func (s *Server) Connection() *jsonrpc2.Conn {
	s.connMu.RLock()
	defer s.connMu.RUnlock()
	return s.conn
}

// SendAsync pushes a notification to the client from any goroutine. It is
// safe to call after a request handler has returned, unlike glsp.Context.Notify.
func (s *Server) SendAsync(method string, params any) {
	conn := s.Connection()
	if conn == nil {
		return
	}
	fmt.Fprintf(os.Stderr, "DBG SendAsync enter %s\n", method)
	if err := conn.Notify(context.Background(), method, params); err != nil && s.logger != nil {
		s.logger.Error("async notification failed", "method", method, "error", err.Error())
	}
	fmt.Fprintf(os.Stderr, "DBG SendAsync exit %s\n", method)
}

// CallRequest sends a request to the client and waits for its reply, since
// for a request-type message the server is only allowed to act once the client
// acknowledges. Returns the client's error (e.g. -32601 "method not found")
// when it does not support the request. Use a generous timeout: some clients
// never answer and would otherwise block the caller forever.
func (s *Server) CallRequest(timeout time.Duration, method string, params any) error {
	conn := s.Connection()
	if conn == nil {
		return errors.New("no connection attached")
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	return conn.Call(ctx, method, params, nil)
}

func (s *Server) Document(uri string) *Document {
	return s.GetDocument(uri)
}

// Completion handles textDocument/completion requests, delegating to the
// code-completion feature package.
func (s *Server) Completion(
	context *glsp.Context,
	params *protocol.CompletionParams,
) (any, error) {
	uri := string(params.TextDocument.URI)
	doc := s.GetDocument(uri)
	if doc == nil {
		return nil, nil
	}

	cursorLine := int(params.Position.Line) + 1
	cursorCol := int(params.Position.Character) + 1

	if s.indexer == nil {
		return codecompletion.Handle(doc.Text, cursorLine, cursorCol)
	}
	return codecompletion.HandleWithIndex(doc.Text, cursorLine, cursorCol, s.indexer)
}

// CodeAction handles textDocument/codeAction requests, delegating to the
// code-action feature package (which returns raw CodeAction values).
func (s *Server) CodeAction(context *glsp.Context, params *protocol.CodeActionParams) (any, error) {
	uri := string(params.TextDocument.URI)
	doc := s.GetDocument(uri)
	if doc == nil {
		return []protocol.CodeAction{}, nil
	}

	var resourceNames, fileNames []string
	if s.indexer != nil {
		resourceNames = s.indexer.ResourceNames()
		fileNames = s.indexer.FileNames()
	}

	actions := codeaction.Handle(uri, doc.Text, params.Context.Diagnostics, params.Range, s.diagnosticsForCached(uri, doc.Text), resourceNames, fileNames)
	return actions, nil
}

// Definition handles textDocument/definition requests.
func (s *Server) Definition(
	context *glsp.Context,
	params *protocol.DefinitionParams,
) (any, error) {
	uri := string(params.TextDocument.URI)
	doc := s.GetDocument(uri)
	if doc == nil {
		return nil, nil
	}

	cursorLine := int(params.Position.Line) + 1
	cursorCol := int(params.Position.Character) + 1

	locs := codedefinition.Handle(doc.Text, cursorLine, cursorCol, uri, s.indexer)
	return locs, nil
}

// Hover handles textDocument/hover requests.
func (s *Server) Hover(
	context *glsp.Context,
	params *protocol.HoverParams,
) (*protocol.Hover, error) {
	uri := string(params.TextDocument.URI)
	doc := s.GetDocument(uri)
	if doc == nil {
		return nil, nil
	}

	cursorLine := int(params.Position.Line) + 1
	cursorCol := int(params.Position.Character) + 1

	return hover.Handle(doc.Text, cursorLine, cursorCol), nil
}
