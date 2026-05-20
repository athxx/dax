package dax

// Router is a high-performance r.
type Router[T any] struct {
	get    Tree[T]
	post   Tree[T]
	delete Tree[T]
	put    Tree[T]
	patch  Tree[T]
}

// NewRouter creates a new router containing trees for every HTTP method.
func NewRouter[T any]() *Router[T] {
	return &Router[T]{}
}

// Add registers a new handler for the given method and path.
func (r *Router[T]) Add(method string, path string, handler T) {
	tree := r.selectTree(method)
	tree.Add(path, handler)
}

// Lookup finds the handler and parameters for the given route.
func (r *Router[T]) Lookup(method string, path string) (T, []Parameter) {
	if method[0] == 'G' {
		return r.get.Lookup(path)
	}

	tree := r.selectTree(method)
	return tree.Lookup(path)
}

// LookupNoAlloc finds the handler and parameters for the given route without using any memory allocations.
func (r *Router[T]) LookupNoAlloc(method string, path string, addParameter func(string, string)) T {
	if method[0] == 'G' {
		return r.get.LookupNoAlloc(path, addParameter)
	}

	tree := r.selectTree(method)
	return tree.LookupNoAlloc(path, addParameter)
}

// Map traverses all trees and calls the given function on every node.
func (r *Router[T]) Map(transform func(T) T) {
	r.get.Map(transform)
	r.post.Map(transform)
	r.delete.Map(transform)
	r.put.Map(transform)
	r.patch.Map(transform)
}

// selectTree returns the tree by the given HTTP method.
func (r *Router[T]) selectTree(method string) *Tree[T] {
	switch method {
	case "GET":
		return &r.get
	case "POST":
		return &r.post
	case "DELETE":
		return &r.delete
	case "PUT":
		return &r.put
	case "PATCH":
		return &r.patch
	default:
		return nil
	}
}
