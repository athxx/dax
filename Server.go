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
	"syscall"
	"time"
)

// SO_REUSEPORT allows multiple processes to bind to the same address.
// Value is 15 on Linux (from <netinet/tcp.h> or <bits/socket.h>).
const soReusePort = 0x0F

// EnvPreforkChild is the environment variable set on prefork child processes.
const EnvPreforkChild = "DAX_PID"

// Server is the interface for an HTTP server.
type Server interface {
	All(path string, handler Handler)
	Delete(path string, handler Handler)
	Get(path string, handler Handler)
	NotFound(handler Handler)
	Patch(path string, handler Handler)
	Post(path string, handler Handler)
	Put(path string, handler Handler)
	Prefork()
	Ready() chan struct{}
	Request(method string, path string, headers []Header, body io.Reader) Response
	Router() *Router[Handler]
	Run(address string) error
	SetErrorHandler(handler func(Context, error))
	Stop()
	Use(handlers ...Handler)
}

// HTTP server.
type server struct {
	handlers     []Handler
	contextPool  sync.Pool
	notFound     Handler
	prefork      bool
	router       *Router[Handler]
	errorHandler func(Context, error)
	ready        chan struct{}
	stop         chan struct{}
	bufferPool   sync.Pool // Reuse bytes.Buffer to reduce GC pressure.
	routeSeq     map[string]uint
	useSeq       uint
}

// create a new server.
func NewServer() Server {
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
		errorHandler: func(ctx Context, err error) {
			log.Println(ctx.Request().Path(), err)
		},
		ready: make(chan struct{}),
		stop:  make(chan struct{}),
		bufferPool: sync.Pool{
			New: func() any { return new(bytes.Buffer) },
		},
		routeSeq: make(map[string]uint),
	}

	s.contextPool.New = func() any { return s.newContext() }
	return s
}

// register all HTTP methods.
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

// Delete requests.
func (s *server) Delete(path string, handler Handler) {
	s.Router().Add("DELETE", path, handler)
	s.recordRoute("DELETE", path)
}

// Get requests.
func (s *server) Get(path string, handler Handler) {
	s.Router().Add("GET", path, handler)
	s.recordRoute("GET", path)
}

// Patch requests.
func (s *server) Patch(path string, handler Handler) {
	s.Router().Add("PATCH", path, handler)
	s.recordRoute("PATCH", path)
}

// Put requests.
func (s *server) Put(path string, handler Handler) {
	s.Router().Add("PUT", path, handler)
	s.recordRoute("PUT", path)
}

// Post requests.
func (s *server) Post(path string, handler Handler) {
	s.Router().Add("POST", path, handler)
	s.recordRoute("POST", path)
}

// recordRoute records the sequence number of a route registration.
func (s *server) recordRoute(method, path string) {
	s.routeSeq[method+":"+path] = s.useSeq
}

// NotFoundHandler sets a custom handler for 404 responses.
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

// Ready returns a channel that will be closed once the listener is ready for connection handling.
func (s *server) Ready() chan struct{} {
	return s.ready
}

// Request performs a synthetic request and returns the response.
// This function keeps the response in memory so it's slightly slower than a real request.
// However it is very useful inside tests where you don't want to spin up a real web server.
func (s *server) Request(method string, url string, headers []Header, body io.Reader) Response {
	ctx := s.newContext()
	ctx.request.headers = headers
	ctx.request.reader.Reset(body)
	s.handleRequest(ctx, method, url, io.Discard)
	return ctx.Response()
}

// Prefork enables multi-process prefork mode using SO_REUSEPORT.
// When enabled, the server forks one child process per GOMAXPROCS
// and the kernel load-balances connections across them.
func (s *server) Prefork() {
	s.prefork = true
}

// logStartup prints server startup info.
func (s *server) logStartup(address string, isChild bool, childIndex, totalChildren int) {
	// fmt.Printf("\n---------%s, %t, %d, %d-----------\n", address, isChild, childIndex, totalChildren)
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

// Run starts the server on the given address.
func (s *server) Run(address string) error {
	if s.prefork && os.Getenv(EnvPreforkChild) == "" {
		return s.runPrefork(address)
	}

	return s.runSingle(address)
}

// runPrefork forks child processes with SO_REUSEPORT.
func (s *server) runPrefork(address string) error {
	numCPU := runtime.GOMAXPROCS(0)
	childProcs := make([]*os.Process, 0, numCPU)

	s.logStartup(address, false, 0, numCPU)

	for i := 0; i < numCPU; i++ {
		env := os.Environ()
		env = append(env, EnvPreforkChild+`=`+strconv.FormatInt(int64(i+1), 10))

		proc, err := os.StartProcess(os.Args[0], os.Args, &os.ProcAttr{
			Env:   env,
			Files: []*os.File{os.Stdin, os.Stdout, os.Stderr},
		})
		if err != nil {
			return fmt.Errorf("dax: prefork failed to start child %d: %w", i+1, err)
		}
		childProcs = append(childProcs, proc)
	}

	// Wait for all children to exit
	for _, proc := range childProcs {
		proc.Wait()
	}

	return nil
}

// runSingle runs the server in a single process (normal or prefork child).
func (s *server) runSingle(address string) error {
	var listener net.Listener
	var err error

	isChild := os.Getenv(EnvPreforkChild)
	childIndex := 0
	totalChildren := 0

	if s.prefork && isChild != "" {
		childIndex, _ = strconv.Atoi(isChild)
		totalChildren = runtime.GOMAXPROCS(0)

		config := &net.ListenConfig{
			Control: func(network, address string, c syscall.RawConn) error {
				return c.Control(func(fd uintptr) {
					syscall.SetsockoptInt(int(fd), syscall.SOL_SOCKET, syscall.SO_REUSEADDR, 1)
					syscall.SetsockoptInt(int(fd), syscall.SOL_SOCKET, soReusePort, 1)
				})
			},
		}

		listener, err = config.Listen(goctx.Background(), "tcp", address)
	} else {
		listener, err = net.Listen("tcp", address)
	}

	if err != nil {
		return err
	}

	defer listener.Close()

	s.logStartup(address, isChild != "", childIndex, totalChildren)

	go func() {
		for {
			conn, err := listener.Accept()

			if err != nil {
				continue
			}

			go s.handleConnection(conn)
		}
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	close(s.ready)

	select {
	case <-stop:
	case <-s.stop:
	}

	return nil
}

// Stop shuts down the server gracefully.
func (s *server) Stop() {
	close(s.stop)
}

// Router returns the router used by the server.
func (s *server) Router() *Router[Handler] {
	return s.router
}

// Use adds handlers to your handlers chain.
// Middleware only applies to routes registered after the Use() call.
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

// SetErrorHandler sets the error handler for the server.
func (s *server) SetErrorHandler(handler func(Context, error)) {
	s.errorHandler = handler
}

// handleConnection handles an accepted connection.
func (s *server) handleConnection(conn net.Conn) {
	var (
		ctx    = s.contextPool.Get().(*context)
		method string
		url    string
		close  bool
	)

	ctx.reader.Reset(conn)

	defer conn.Close()
	defer s.contextPool.Put(ctx)

	for !close {
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

// handleRequest handles the given request.
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
		s.errorHandler(ctx, err)
	}

	// Reuse buffer to reduce GC pressure
	buf := s.bufferPool.Get().(*bytes.Buffer)
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
	s.bufferPool.Put(buf)
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
