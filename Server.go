package dax

import (
	"bytes"
	goctx "context"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"os/signal"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
)

// SO_REUSEPORT allows multiple processes to bind to the same address.
// Value is 15 on Linux (from <netinet/tcp.h> or <bits/socket.h>).
const soReusePort = 0x0F

// EnvPreforkChild is the environment variable set on prefork child processes.
const EnvPreforkChild = "DAX_PID"

// Server is an HTTP server interface.
type Server interface {
	All(path string, handler Handler)
	Delete(path string, handler Handler)
	Get(path string, handler Handler)
	NotFound(handler Handler)
	Patch(path string, handler Handler)
	Post(path string, handler Handler)
	Put(path string, handler Handler)
	Ready() chan struct{}
	Request(method string, path string, headers []Header, body io.Reader) Response
	Router() *Router[Handler]
	Run(address string) error
	RunUnix(socketPath string) error
	SetErrorHandler(handler func(Context, error))
	Stop()
	Use(handlers ...Handler)
}

// server implements the Server interface.
type server struct {
	handlers    []Handler
	ctxPool     sync.Pool
	notFound    Handler
	bootMsg     bool
	prefork     bool
	preforkNum  int
	router      *Router[Handler]
	errHandler  func(Context, error)
	ready       chan struct{}
	stop        chan struct{}
	bufPool     sync.Pool // Reuse bytes.Buffer to reduce GC pressure.
	routeSeq    map[string]uint
	useSeq      uint
	conns       sync.WaitGroup        // Tracks in-flight connections for graceful shutdown.
	shutdown    atomic.Bool           // Signals handleConnection to stop reading new requests.
	connsMu     sync.Mutex            // Guards activeConns.
	activeConns map[net.Conn]struct{} // Open connections, used to force-close on shutdown timeout.
	graceful    bool                  // Whether graceful shutdown is enabled.
	gracefulTTL time.Duration         // Max time to wait for in-flight requests; 0 = wait forever.
}

type Config struct {
	// EnablePrefork enables multi-process mode. See Server.Prefork() for details.
	EnablePrefork bool
	// PreforkNum specifies the number of child processes to fork in prefork mode.
	// If zero, defaults to the number of CPUs.
	PreforkNum int
	// EnableBootMsg enables the startup message with server info and PID.
	EnableBootMsg bool
	// EnableGracefulShutdown enables graceful shutdown on SIGINT/SIGTERM, waiting for in-flight
	// requests to finish before exiting. See Server.SetShutdownTimeout() for details.
	EnableGracefulShutdown bool
	// GracefulShutdownTimeout sets the maximum time Server.Stop() will wait for in-flight
	// requests to finish before force-closing remaining connections. Zero means wait forever (the default).
	GracefulShutdownTimeout time.Duration
}

// NewServer creates a new Server.
func NewServer(cfg ...*Config) Server {
	config := &Config{}
	if len(cfg) > 0 {
		c := cfg[0]
		if c != nil {
			config = c
		}
	}

	r := &Router[Handler]{}
	s := &server{
		router: r,
		handlers: []Handler{
			func(ctx Context) error {
				handler := ctx.Handler()
				if handler == nil {
					ctx.Status(404)
					ctx.Response().SetHeader("Content-Type", "text/plain")
					ctx.Response().SetBody([]byte("404 Not Found"))
					return nil
				}
				return handler(ctx)
			},
		},
		errHandler: func(ctx Context, err error) {
			log.Println(ctx.Request().Path(), err)
		},
		ready:       make(chan struct{}),
		stop:        make(chan struct{}),
		activeConns: make(map[net.Conn]struct{}),
		bufPool: sync.Pool{
			New: func() any { return new(bytes.Buffer) },
		},
		routeSeq:    make(map[string]uint),
		bootMsg:     config.EnableBootMsg,
		prefork:     config.EnablePrefork,
		preforkNum:  config.PreforkNum,
		graceful:    config.EnableGracefulShutdown,
		gracefulTTL: config.GracefulShutdownTimeout,
	}
	s.ctxPool.New = func() any { return s.newContext() }
	return s
}

// All registers handler for all HTTP methods on the given path.
func (s *server) All(path string, handler Handler) {
	s.Router().Add("DELETE", path, handler)
	s.Router().Add("GET", path, handler)
	s.Router().Add("PATCH", path, handler)
	s.Router().Add("POST", path, handler)
	s.Router().Add("PUT", path, handler)
	s.recordRoute("DELETE", path)
	s.recordRoute("GET", path)
	s.recordRoute("PATCH", path)
	s.recordRoute("POST", path)
	s.recordRoute("PUT", path)
}

// Delete registers handler for DELETE requests on the given path.
func (s *server) Delete(path string, handler Handler) {
	s.Router().Add("DELETE", path, handler)
	s.recordRoute("DELETE", path)
}

// Get registers handler for GET requests on the given path.
func (s *server) Get(path string, handler Handler) {
	s.Router().Add("GET", path, handler)
	s.recordRoute("GET", path)
}

// Patch registers handler for PATCH requests on the given path.
func (s *server) Patch(path string, handler Handler) {
	s.Router().Add("PATCH", path, handler)
	s.recordRoute("PATCH", path)
}

// Put registers handler for PUT requests on the given path.
func (s *server) Put(path string, handler Handler) {
	s.Router().Add("PUT", path, handler)
	s.recordRoute("PUT", path)
}

// Post registers handler for POST requests on the given path.
func (s *server) Post(path string, handler Handler) {
	s.Router().Add("POST", path, handler)
	s.recordRoute("POST", path)
}

// recordRoute records the sequence number of a route registration.
func (s *server) recordRoute(method, path string) {
	s.routeSeq[method+":"+path] = s.useSeq
}

// NotFound registers a custom 404 handler.
func (s *server) NotFound(h Handler) {
	s.notFound = h
	// Update the terminal handler in the chain to use the custom handler
	s.handlers[len(s.handlers)-1] = func(ctx Context) error {
		h := ctx.Handler()
		if h == nil {
			if s.notFound != nil {
				return s.notFound(ctx)
			}
			ctx.Status(404)
			ctx.Response().SetHeader("Content-Type", "text/plain")
			ctx.Response().SetBody([]byte("404 Not Found"))
			return nil
		}
		return h(ctx)
	}
}

// Ready returns a channel closed when the listener is ready.
func (s *server) Ready() chan struct{} {
	return s.ready
}

// Request performs a synthetic request and returns the response.
// Useful in tests — avoids spinning up a real server.
func (s *server) Request(method string, url string, headers []Header, body io.Reader) Response {
	ctx := s.newContext()
	ctx.request.headers = headers
	ctx.request.reader.Reset(body)
	s.handleRequest(ctx, method, url, io.Discard)
	return ctx.Response()
}

// logLogo prints server startup info.
func (s *server) logLogo(address string, isChild bool, childIndex, totalChildren int) {
	if !s.bootMsg {
		return
	}
	if !isChild {
		println(`
▄▄▄▄   ▄▄▄  ▄▄ ▄▄ 
██▀██ ██▀██ ▀█▄█▀ 
████▀ ██▀██ ██ ██ v1.0
-------------------------------------------------------------------------
  Listening on  `, address)
		println("  PID:          ", os.Getpid())
		println("  Processes     ", totalChildren+1)
	}
	if totalChildren > 0 {
		if childIndex == 0 {
			print(`  Child PIDs:    `)
		} else if childIndex > 0 && childIndex < totalChildren {
			print(strconv.FormatInt(int64(os.Getpid()), 10) + `,`)
		}
	}
	if childIndex == totalChildren {
		time.Sleep(5 * time.Millisecond)
		if isChild {
			print(os.Getpid(), "\n")
		}
		println("-------------------------------------------------------------------------\n")
	}
}

// Run starts the server on the given TCP address.
func (s *server) Run(address string) error {
	return s.run("tcp", address)
}

// RunUnix starts the server on a Unix domain socket at the given path.
func (s *server) RunUnix(socketPath string) error {
	return s.run("unix", socketPath)
}

// run starts the server, dispatching to prefork or single-process mode.
func (s *server) run(network, address string) error {
	if s.prefork && os.Getenv(EnvPreforkChild) == "" {
		return s.runPrefork(network, address)
	}
	return s.runSingle(network, address)
}

// runPrefork forks child processes. TCP uses SO_REUSEPORT (each child binds);
// Unix sockets share the parent's listener fd via fd inheritance.
func (s *server) runPrefork(network, address string) error {
	numCPU := runtime.GOMAXPROCS(0)
	if s.preforkNum > 0 {
		numCPU = s.preforkNum
	}
	files := []*os.File{os.Stdin, os.Stdout, os.Stderr}
	// Unix sockets can't use SO_REUSEPORT — share one listener fd with children.
	if network == "unix" {
		os.Remove(address)
		l, err := net.Listen("unix", address)
		if err != nil {
			return err
		}
		defer l.Close()
		f, err := l.(*net.UnixListener).File()
		if err != nil {
			return err
		}
		defer f.Close()
		files = append(files, f)
	}
	childProcs := make([]*os.Process, 0, numCPU)
	s.logLogo(address, false, 0, numCPU)
	for i := 0; i < numCPU; i++ {
		env := append(os.Environ(), EnvPreforkChild+`=`+strconv.FormatInt(int64(i+1), 10))
		proc, err := os.StartProcess(os.Args[0], os.Args, &os.ProcAttr{
			Env:   env,
			Files: files,
		})
		if err != nil {
			return fmt.Errorf("dax: prefork failed to start child %d: %w", i+1, err)
		}
		childProcs = append(childProcs, proc)
	}
	for _, proc := range childProcs {
		proc.Wait()
	}
	return nil
}

// runSingle runs the server in a single process.
func (s *server) runSingle(network, address string) error {
	var listener net.Listener
	var err error
	isChild := os.Getenv(EnvPreforkChild)
	childIndex := 0
	totalChildren := 0
	if s.prefork && isChild != "" {
		childIndex, _ = strconv.Atoi(isChild)
		totalChildren = runtime.GOMAXPROCS(0)
		if network == "unix" {
			// Unix prefork child: inherit listener fd from parent (fd 3).
			f := os.NewFile(3, "listener")
			listener, err = net.FileListener(f)
			f.Close()
		} else {
			config := &net.ListenConfig{
				Control: func(network, address string, c syscall.RawConn) error {
					return c.Control(func(fd uintptr) {
						syscall.SetsockoptInt(int(fd), syscall.SOL_SOCKET, syscall.SO_REUSEADDR, 1)
						syscall.SetsockoptInt(int(fd), syscall.SOL_SOCKET, soReusePort, 1)
					})
				},
			}
			listener, err = config.Listen(goctx.Background(), network, address)
		}
	} else {
		if network == "unix" {
			os.Remove(address)
		}
		listener, err = net.Listen(network, address)
	}
	if err != nil {
		return err
	}
	s.logLogo(address, isChild != "", childIndex, totalChildren)
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				if s.shutdown.Load() {
					return
				}
				continue
			}
			s.conns.Add(1)
			s.connsMu.Lock()
			s.activeConns[conn] = struct{}{}
			s.connsMu.Unlock()
			go func() {
				defer func() {
					s.connsMu.Lock()
					delete(s.activeConns, conn)
					s.connsMu.Unlock()
					s.conns.Done()
				}()
				s.handleConnection(conn)
			}()
		}
	}()
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	close(s.ready)
	select {
	case <-stop:
	case <-s.stop:
	}
	// Stop accepting new connections immediately.
	s.shutdown.Store(true)
	listener.Close()
	if !s.graceful {
		// Graceful shutdown disabled: force-close every active connection and return.
		s.connsMu.Lock()
		for c := range s.activeConns {
			c.Close()
		}
		s.connsMu.Unlock()
		return nil
	}
	// Graceful: wake up keep-alive connections idling in Read so they exit their
	// loop. In-flight handlers keep running until they finish or gracefulTTL fires.
	s.connsMu.Lock()
	for c := range s.activeConns {
		c.SetReadDeadline(time.Now())
	}
	s.connsMu.Unlock()
	done := make(chan struct{})
	go func() {
		s.conns.Wait()
		close(done)
	}()
	if s.gracefulTTL > 0 {
		select {
		case <-done:
		case <-time.After(s.gracefulTTL):
			s.connsMu.Lock()
			for c := range s.activeConns {
				c.Close()
			}
			s.connsMu.Unlock()
			<-done
		}
	} else {
		<-done
	}
	return nil
}

// Stop gracefully shuts down the server, waiting for in-flight requests to finish.
func (s *server) Stop() {
	select {
	case <-s.stop:
		// Already stopped.
	default:
		close(s.stop)
	}
}

// Router returns the server's router.
func (s *server) Router() *Router[Handler] {
	return s.router
}

// Use registers middleware. Only applies to routes registered after this call.
func (s *server) Use(handlers ...Handler) {
	s.useSeq++
	boundary := s.useSeq
	wrapped := make([]Handler, len(handlers))
	for i, h := range handlers {
		h := h // capture
		wrapped[i] = func(ctx Context) error {
			// Skip this middleware if the route was registered before Use().
			if seq, ok := s.routeSeq[ctx.Request().Method()+":"+ctx.Request().Path()]; ok && seq < boundary {
				return ctx.Next(ctx)
			}
			return h(ctx)
		}
	}
	last := s.handlers[len(s.handlers)-1]
	s.handlers = append(s.handlers[:len(s.handlers)-1], wrapped...)
	s.handlers = append(s.handlers, last)
}

// SetErrorHandler sets a custom error handler.
func (s *server) SetErrorHandler(handler func(Context, error)) {
	s.errHandler = handler
}

// handleConnection reads HTTP requests from a connection and dispatches them.
func (s *server) handleConnection(conn net.Conn) {
	var (
		ctx    = s.ctxPool.Get().(*context)
		method string
		url    string
		close  bool
	)
	ctx.reader.Reset(conn)
	defer conn.Close()
	defer s.ctxPool.Put(ctx)
	for !close {
		if s.shutdown.Load() {
			return
		}
		// Read the HTTP request line
		message, err := ctx.reader.ReadString('\n')
		if err != nil {
			return
		}
		space := strings.IndexByte(message, ' ')
		if space <= 0 {
			io.WriteString(conn, "HTTP/1.1 400 Bad Request\r\n\r\n")
			return
		}
		method = message[:space]
		if !isRequestMethod(method) {
			io.WriteString(conn, "HTTP/1.1 400 Bad Request\r\n\r\n")
			return
		}
		lastSpace := strings.LastIndexByte(message, ' ')
		if lastSpace == space {
			lastSpace = len(message) - len("\r\n")
		}
		space += 1
		if space > lastSpace {
			io.WriteString(conn, "HTTP/1.1 400 Bad Request\r\n\r\n")
			return
		}
		url = message[space:lastSpace]
		// Add headers until we meet an empty line
		for {
			message, err = ctx.reader.ReadString('\n')
			if err != nil {
				return
			}
			if message == "\r\n" {
				break
			}
			colon := strings.IndexByte(message, ':')
			if colon <= 0 {
				continue
			}
			if colon > len(message)-4 {
				continue
			}
			key := message[:colon]
			value := message[colon+2 : len(message)-2]
			ctx.request.headers = append(ctx.request.headers, Header{
				Key:   key,
				Value: value,
			})
			if value == "close" && strings.EqualFold(key, "connection") {
				close = true
			}
		}
		// Handle the request
		s.handleRequest(ctx, method, url, conn)
		// Clean up the context
		ctx.request.consumed = 0
		ctx.request.length = 0
		ctx.request.headers = ctx.request.headers[:0]
		ctx.request.ip = conn.RemoteAddr().String()
		ctx.response.headers = ctx.response.headers[:0]
		ctx.response.body = ctx.response.body[:0]
		ctx.params = ctx.params[:0]
		ctx.handlerCount = 0
		ctx.status = 200
	}
}

// handleRequest dispatches the request to middleware or the terminal handler.
// Non-existent routes skip middleware and go directly to the terminal (404) handler.
func (s *server) handleRequest(ctx *context, method string, url string, w io.Writer) {
	ctx.method = method
	ctx.scheme, ctx.host, ctx.path, ctx.query = parseURL(url)
	// If the route doesn't exist, skip middleware and go directly to the
	// terminal handler (404). This prevents auth/rate-limit middlewares
	// from intercepting non-existent routes.
	var h Handler
	handler := s.handlers[len(s.handlers)-1]
	if h = s.router.LookupNoAlloc(method, ctx.path, func(string, string) {}); h != nil {
		handler = s.handlers[0]
	}
	_ = h
	err := handler(ctx)
	if err != nil {
		s.errHandler(ctx, err)
	}
	// Reuse buffer to reduce GC pressure
	buf := s.bufPool.Get().(*bytes.Buffer)
	buf.Reset()
	buf.WriteString(`HTTP/1.1 `)
	buf.WriteString(strconv.Itoa(int(ctx.status)))
	buf.WriteString("\r\nContent-Length: ")
	buf.WriteString(strconv.Itoa(len(ctx.response.body)))
	buf.WriteString("\r\n")
	for _, header := range ctx.response.headers {
		buf.WriteString(header.Key)
		buf.WriteString(`: `)
		buf.WriteString(header.Value)
		buf.WriteString("\r\n")
	}
	buf.WriteString("\r\n")
	buf.Write(ctx.response.body)
	w.Write(buf.Bytes())
	s.bufPool.Put(buf)
}

// newContext allocates a new context with the default state.
func (s *server) newContext() *context {
	return &context{
		server: s,
		request: request{
			headers: make([]Header, 0, 16), // Preallocate larger capacity to reduce resizing
			params:  make([]Parameter, 0, 8),
		},
		response: response{
			body:    make([]byte, 0, 2048), // Preallocate larger capacity
			headers: make([]Header, 0, 8),
			status:  200,
		},
	}
}
