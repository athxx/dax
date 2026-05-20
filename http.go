package dax

import "strings"

// Query is a part of a URL that assigns values to specified parameters.
type Query string

// Param retrieves a query parameter.
func (q Query) Param(name string) string {
	query := strings.ReplaceAll(string(q), "+", " ")

	for pair := range strings.SplitSeq(query, "&") {
		if pair == "" {
			continue
		}

		key := pair
		value := ""
		equal := strings.IndexByte(pair, '=')

		if equal != -1 {
			key = pair[:equal]
			value = pair[equal+1:]
		}

		if key == name {
			return value
		}
	}

	return ""
}

// Header is used to store HTTP headers.
type Header struct {
	Key   string
	Value string
}

// Handler is a function that deals with the given request/response context.
type Handler func(Context) error

// isRequestMethod returns true if the given string is a valid HTTP request method.
func isRequestMethod(method string) bool {
	switch method {
	case "GET", "HEAD", "POST", "PUT", "DELETE", "CONNECT", "OPTIONS", "TRACE", "PATCH":
		return true
	default:
		return false
	}
}

// parseURL parses a URL and returns the scheme, host, path and query.
func parseURL(url string) (scheme string, host string, path string, query Query) {
	schemePos := strings.Index(url, "://")

	if schemePos != -1 {
		scheme = url[:schemePos]
		url = url[schemePos+len("://"):]
	}

	pathPos := strings.IndexByte(url, '/')

	if pathPos != -1 {
		host = url[:pathPos]
		url = url[pathPos:]
	}

	queryPos := strings.IndexByte(url, '?')

	if queryPos != -1 {
		path = url[:queryPos]
		query = Query(url[queryPos+1:])
		return
	}

	path = url
	return
}
