package dax

import (
	"strings"
)

// node types
const separator, parameter, wildcard = '/', ':', '*'

// treeNode is a radix tree n.
type treeNode[T any] struct {
	prefix     string
	data       T
	children   []*treeNode[T]
	parameter  *treeNode[T]
	wildcard   *treeNode[T]
	indices    []uint8
	startIndex uint8
	endIndex   uint8
	kind       byte
}

// split splits the node at the given index and inserts
// a new child node with the given path and data.
// If path is empty, it will not create another child node
// and instead assign the data directly to the n.
func (n *treeNode[T]) split(index int, path string, data T) {
	// Create split node with the remaining string
	splitNode := n.clone(n.prefix[index:])

	// The existing data must be removed
	n.reset(n.prefix[:index])

	// If the path is empty, it means we don't create a 2nd child n.
	// Just assign the data for the existing node and store a single child n.
	if path == "" {
		n.data = data
		n.addChild(splitNode)
		return
	}

	n.addChild(splitNode)

	// Create new nodes with the remaining path
	n.append(path, data)
}

// clone clones the node with a new prefix.
func (n *treeNode[T]) clone(prefix string) *treeNode[T] {
	return &treeNode[T]{
		prefix:     prefix,
		data:       n.data,
		indices:    n.indices,
		startIndex: n.startIndex,
		endIndex:   n.endIndex,
		children:   n.children,
		parameter:  n.parameter,
		wildcard:   n.wildcard,
		kind:       n.kind,
	}
}

// reset resets the existing node data.
func (n *treeNode[T]) reset(prefix string) {
	var empty T
	n.prefix = prefix
	n.data = empty
	n.parameter = nil
	n.wildcard = nil
	n.kind = 0
	n.startIndex = 0
	n.endIndex = 0
	n.indices = nil
	n.children = nil
}

// addChild adds a child tree.
func (n *treeNode[T]) addChild(child *treeNode[T]) {
	if len(n.children) == 0 {
		n.children = append(n.children, nil)
	}

	firstChar := child.prefix[0]

	switch {
	case n.startIndex == 0:
		n.startIndex = firstChar
		n.indices = []uint8{0}
		n.endIndex = n.startIndex + uint8(len(n.indices))

	case firstChar < n.startIndex:
		diff := n.startIndex - firstChar
		newIndices := make([]uint8, diff+uint8(len(n.indices)))
		copy(newIndices[diff:], n.indices)
		n.startIndex = firstChar
		n.indices = newIndices
		n.endIndex = n.startIndex + uint8(len(n.indices))

	case firstChar >= n.endIndex:
		diff := firstChar - n.endIndex + 1
		newIndices := make([]uint8, diff+uint8(len(n.indices)))
		copy(newIndices, n.indices)
		n.indices = newIndices
		n.endIndex = n.startIndex + uint8(len(n.indices))
	}

	index := n.indices[firstChar-n.startIndex]

	if index == 0 {
		n.indices[firstChar-n.startIndex] = uint8(len(n.children))
		n.children = append(n.children, child)
		return
	}

	n.children[index] = child
}

// addTrailingSlash adds a trailing slash with the same data.
func (n *treeNode[T]) addTrailingSlash(data T) {
	if strings.HasSuffix(n.prefix, "/") || n.kind == wildcard || (separator >= n.startIndex && separator < n.endIndex && n.indices[separator-n.startIndex] != 0) {
		return
	}

	n.addChild(&treeNode[T]{
		prefix: "/",
		data:   data,
	})
}

// append appends the given path to the tree.
func (n *treeNode[T]) append(path string, data T) {
	// At this point, all we know is that somewhere
	// in the remaining string we have parameters.
	// node: /user|
	// path: /user|/:userid
	for {
		if path == "" {
			n.data = data
			return
		}

		paramStart := strings.IndexByte(path, parameter)

		if paramStart == -1 {
			paramStart = strings.IndexByte(path, wildcard)
		}

		// If it's a static route we are adding,
		// just add the remainder as a normal n.
		if paramStart == -1 {
			// If the node itself doesn't have a prefix (root node),
			// don't add a child and use the node itself.
			if n.prefix == "" {
				n.prefix = path
				n.data = data
				n.addTrailingSlash(data)
				return
			}

			child := &treeNode[T]{
				prefix: path,
				data:   data,
			}

			n.addChild(child)
			child.addTrailingSlash(data)
			return
		}

		// If we're directly in front of a parameter,
		// add a parameter n.
		if paramStart == 0 {
			paramEnd := strings.IndexByte(path, separator)

			if paramEnd == -1 {
				paramEnd = len(path)
			}

			child := &treeNode[T]{
				prefix: path[1:paramEnd],
				kind:   path[paramStart],
			}

			switch child.kind {
			case parameter:
				child.addTrailingSlash(data)
				n.parameter = child
				n = child
				path = path[paramEnd:]
				continue

			case wildcard:
				child.data = data
				n.wildcard = child
				return
			}
		}

		// We know there's a parameter, but not directly at the start.

		// If the node itself doesn't have a prefix (root node),
		// don't add a child and use the node itself.
		if n.prefix == "" {
			n.prefix = path[:paramStart]
			path = path[paramStart:]
			continue
		}

		// Add a normal node with the path before the parameter start.
		child := &treeNode[T]{
			prefix: path[:paramStart],
		}

		// Allow trailing slashes to return
		// the same content as their parent n.
		if child.prefix == "/" {
			child.data = n.data
		}

		n.addChild(child)
		n = child
		path = path[paramStart:]
	}
}

// end is called when the node was fully parsed
// and needs to decide the next control flow.
// end is only called from `tree.Add`.
func (n *treeNode[T]) end(path string, data T, i int, offset int) (*treeNode[T], int, flow) {
	char := path[i]

	if char >= n.startIndex && char < n.endIndex {
		index := n.indices[char-n.startIndex]

		if index != 0 {
			n = n.children[index]
			offset = i
			return n, offset, flowNext
		}
	}

	// No fitting children found, does this node even contain a prefix yet?
	// If no prefix is set, this is the starting n.
	if n.prefix == "" {
		n.append(path[i:], data)
		return n, offset, flowStop
	}

	// node: /user/|:id
	// path: /user/|:id/profile
	if n.parameter != nil && path[i] == parameter {
		n = n.parameter
		offset = i
		return n, offset, flowBegin
	}

	n.append(path[i:], data)
	return n, offset, flowStop
}

// each traverses the tree and calls the given function on every n.
func (n *treeNode[T]) each(callback func(*treeNode[T])) {
	callback(n)

	for _, child := range n.children {
		if child == nil {
			continue
		}

		child.each(callback)
	}

	if n.parameter != nil {
		n.parameter.each(callback)
	}

	if n.wildcard != nil {
		n.wildcard.each(callback)
	}
}
