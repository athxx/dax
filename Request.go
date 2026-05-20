package dax

import (
	"bufio"
	"io"
	"strconv"
	"strings"
)

// Request is an interface for HTTP requests.
type Request interface {
	io.Reader
	Header(string) string
	Host() string
	Method() string
	Path() string
	Scheme() string
	Param(string) string
	Query() Query
}

// request represents the HTTP request used in the given context.
type request struct {
	reader   bufio.Reader
	scheme   string
	host     string
	method   string
	path     string
	query    Query
	headers  []Header
	params   []Parameter
	length   int
	consumed int
}

// Header returns the header value for the given key.
func (req *request) Header(key string) string {
	for _, header := range req.headers {
		if strings.EqualFold(header.Key, key) {
			return header.Value
		}
	}

	return ""
}

// Host returns the requested host.
func (req *request) Host() string {
	return req.host
}

// Method returns the request method.
func (req *request) Method() string {
	return req.method
}

// Param retrieves a parameter.
func (req *request) Param(name string) string {
	for i := range len(req.params) {
		p := req.params[i]

		if p.Key == name {
			return p.Value
		}
	}

	return ""
}

// Query returns the query string as a Query type.
func (req *request) Query() Query {
	return req.query
}

// Path returns the requested path.
func (req *request) Path() string {
	return req.path
}

// Read implements the io.Reader interface.
func (req *request) Read(p []byte) (n int, err error) {
	if req.length == 0 {
		req.length, _ = strconv.Atoi(req.Header("Content-Length"))

		if req.length == 0 {
			return 0, io.EOF
		}
	}

	n, err = req.reader.Read(p)
	req.consumed += n

	if req.consumed < req.length {
		return n, err
	}

	return n - (req.consumed - req.length), io.EOF
}

// Scheme returns either `http`, `https` or an empty string.
func (req request) Scheme() string {
	return req.scheme
}

// addParameter adds a new parameter to the request.
func (req *request) addParameter(key string, value string) {
	req.params = append(req.params, Parameter{
		Key:   key,
		Value: value,
	})
}
