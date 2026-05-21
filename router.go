package dax

// Router is a high-performance radix-tree router.
type Router[T any] struct {
	get    Tree[T]
	post   Tree[T]
	delete Tree[T]
	put    Tree[T]
	patch  Tree[T]
}

// NewRouter creates a router with trees per HTTP method.
func NewRouter[T any]() *Router[T] {
	return &Router[T]{}
}

// Add registers a handler for the given method and path.
func (r *Router[T]) Add(method string, path string, handler T) {
	tree := r.selectTree(method)
	tree.Add(path, handler)
}

// Lookup finds the handler and parameters for a route.
func (r *Router[T]) Lookup(method string, path string) (T, []Parameter) {
	if method[0] == 'G' {
		return r.get.Lookup(path)
	}

	tree := r.selectTree(method)
	return tree.Lookup(path)
}

// LookupNoAlloc finds the handler without allocating.
func (r *Router[T]) LookupNoAlloc(method string, path string, addParameter func(string, string)) T {
	if method[0] == 'G' {
		return r.get.LookupNoAlloc(path, addParameter)
	}

	tree := r.selectTree(method)
	return tree.LookupNoAlloc(path, addParameter)
}

// Map calls transform on every handler in the router.
func (r *Router[T]) Map(transform func(T) T) {
	r.get.Map(transform)
	r.post.Map(transform)
	r.delete.Map(transform)
	r.put.Map(transform)
	r.patch.Map(transform)
}

// selectTree returns the tree for the given HTTP method.
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
