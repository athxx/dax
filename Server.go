package dax

import (
	"bytes"
	"io"
	"log"
	"net"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"sync"
	"syscall"
)

// Server is the interface for an HTTP server.
type Server interface {
	All(path string, handler Handler)
	Delete(path string, handler Handler)
	Get(path string, handler Handler)
	Patch(path string, handler Handler)
	Post(path string, handler Handler)
	Put(path string, handler Handler)
	Ready() chan struct{}
	Request(method string, path string, headers []Header, body io.Reader) Response
	Router() *Router[Handler]
	Run(address string) error
	SetErrorHandler(handler func(Context, error))
	Use(handlers ...Handler)
}

// server is an HTTP server.
type server struct {
	handlers     []Handler
	contextPool  sync.Pool
	router       *Router[Handler]
	errorHandler func(Context, error)
	ready        chan struct{}
}

// NewServer creates a new HTTP server.
func NewServer() Server {
	r := &Router[Handler]{}
	s := &server{
		router: r,
		handlers: []Handler{
			func(ctx Context) error {
				handler := ctx.Handler()

				if handler == nil {
					ctx.Status(404)
					return nil
				}

				return handler(ctx)
			},
		},
		errorHandler: func(ctx Context, err error) {
			log.Println(ctx.Request().Path(), err)
		},
		ready: make(chan struct{}),
	}

	s.contextPool.New = func() any { return s.newContext() }
	return s
}

// All registers your `handler` for the given `path` on all HTTP methods.
func (s *server) All(path string, handler Handler) {
	s.Router().Add("DELETE", path, handler)
	s.Router().Add("GET", path, handler)
	s.Router().Add("PATCH", path, handler)
	s.Router().Add("POST", path, handler)
	s.Router().Add("PUT", path, handler)
}

// Delete registers your `handler` for the given `path` on DELETE requests.
func (s *server) Delete(path string, handler Handler) {
	s.Router().Add("DELETE", path, handler)
}

// Get registers your `handler` for the given `path` on GET requests.
func (s *server) Get(path string, handler Handler) {
	s.Router().Add("GET", path, handler)
}

// Patch registers your `handler` for the given `path` on PATCH requests.
func (s *server) Patch(path string, handler Handler) {
	s.Router().Add("PATCH", path, handler)
}

// Put registers your `handler` for the given `path` on PUT requests.
func (s *server) Put(path string, handler Handler) {
	s.Router().Add("PUT", path, handler)
}

// Post registers your `handler` for the given `path` on POST requests.
func (s *server) Post(path string, handler Handler) {
	s.Router().Add("POST", path, handler)
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

// Run starts the server on the given address.
func (s *server) Run(address string) error {
	listener, err := net.Listen("tcp", address)

	if err != nil {
		return err
	}

	defer listener.Close()

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
	<-stop
	return nil
}

// Router returns the router used by the server.
func (s *server) Router() *Router[Handler] {
	return s.router
}

// Use adds handlers to your handlers chain.
func (s *server) Use(handlers ...Handler) {
	last := s.handlers[len(s.handlers)-1]
	s.handlers = append(s.handlers[:len(s.handlers)-1], handlers...)
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
		ctx.response.headers = ctx.response.headers[:0]
		ctx.response.body = ctx.response.body[:0]
		ctx.params = ctx.params[:0]
		ctx.handlerCount = 0
		ctx.status = 200
	}
}

// handleRequest handles the given request.
func (s *server) handleRequest(ctx *context, method string, url string, writer io.Writer) {
	ctx.method = method
	ctx.scheme, ctx.host, ctx.path, ctx.query = parseURL(url)

	err := s.handlers[0](ctx)

	if err != nil {
		s.errorHandler(ctx, err)
	}

	tmp := bytes.Buffer{}
	tmp.WriteString("HTTP/1.1 ")
	tmp.WriteString(strconv.Itoa(int(ctx.status)))
	tmp.WriteString("\r\nContent-Length: ")
	tmp.WriteString(strconv.Itoa(len(ctx.response.body)))
	tmp.WriteString("\r\n")

	for _, header := range ctx.response.headers {
		tmp.WriteString(header.Key)
		tmp.WriteString(": ")
		tmp.WriteString(header.Value)
		tmp.WriteString("\r\n")
	}

	tmp.WriteString("\r\n")
	tmp.Write(ctx.response.body)
	writer.Write(tmp.Bytes())
}

// newContext allocates a new context with the default state.
func (s *server) newContext() *context {
	return &context{
		server: s,
		request: request{
			headers: make([]Header, 0, 8),
			params:  make([]Parameter, 0, 8),
		},
		response: response{
			body:    make([]byte, 0, 1024),
			headers: make([]Header, 0, 8),
			status:  200,
		},
	}
}
