// Package fieldpath turns the fieldsV1 sets found in a Kubernetes object's
// metadata.managedFields into a tree that can be walked alongside the object
// itself, answering "which manager owns this node?" for every node.
//
// A fieldsV1 set is a nested JSON object whose keys name a step down the
// object: "f:<name>" for a map key, "i:<n>" for a list index, "v:<json>" for a
// scalar in a set-typed list, and "k:<json>" for an element of an associative
// list, identified by its key fields. The special key "." means the node the
// keys sit in is itself owned, as opposed to merely being on the way to
// something owned. A node with no keys at all is atomic: the manager owns it
// and everything inside it.
package fieldpath

import (
	"bytes"
	"encoding/json"
	"sort"
	"strconv"
	"strings"
)

// Node is one node of the merged ownership tree. Managers are referred to by
// index, so several fieldsV1 sets can share one tree.
type Node struct {
	owners   map[int]struct{}
	children map[string]*Node

	elems    []elem // parsed list-element keys, lazily built
	elemsSet bool

	subtree    []int // owners of this node and everything below it, lazily built
	subtreeSet bool
}

// elem is a parsed child key that selects a list element.
type elem struct {
	node  *Node
	kind  byte           // 'i', 'v' or 'k'
	index int            // kind 'i'
	value any            // kind 'v'
	match map[string]any // kind 'k'
}

// Set is the ownership tree merged from every manager's fieldsV1.
type Set struct {
	root Node
}

// NewSet returns an empty ownership tree.
func NewSet() *Set { return &Set{} }

// Add merges one manager's decoded fieldsV1 object into the tree. The manager
// is identified by index; the same index may be added more than once, which is
// what happens when one manager holds several managedFields entries.
func (s *Set) Add(manager int, fields map[string]any) {
	addFields(&s.root, manager, fields)
}

func addFields(n *Node, manager int, fields map[string]any) {
	if len(fields) == 0 {
		n.own(manager)
		return
	}
	for k, v := range fields {
		if k == "." {
			n.own(manager)
			continue
		}
		sub, ok := v.(map[string]any)
		if !ok {
			// Not something we understand; treat the key as an owned leaf.
			n.child(k).own(manager)
			continue
		}
		addFields(n.child(k), manager, sub)
	}
}

func (n *Node) own(manager int) {
	if n.owners == nil {
		n.owners = make(map[int]struct{})
	}
	n.owners[manager] = struct{}{}
}

func (n *Node) child(key string) *Node {
	if n.children == nil {
		n.children = make(map[string]*Node)
	}
	c := n.children[key]
	if c == nil {
		c = &Node{}
		n.children[key] = c
	}
	return c
}

func (n *Node) ownerList() []int {
	if len(n.owners) == 0 {
		return nil
	}
	out := make([]int, 0, len(n.owners))
	for m := range n.owners {
		out = append(out, m)
	}
	sort.Ints(out)
	return out
}

// atomic reports whether the node is owned outright, with nothing named below
// it, so everything inside the object at that point belongs to the same owners.
func (n *Node) atomic() bool { return len(n.owners) > 0 && len(n.children) == 0 }

// subtreeOwners is every manager owning this node or anything below it.
func (n *Node) subtreeOwners() []int {
	if n.subtreeSet {
		return n.subtree
	}
	seen := make(map[int]struct{})
	var walk func(*Node)
	walk = func(x *Node) {
		for m := range x.owners {
			seen[m] = struct{}{}
		}
		for _, c := range x.children {
			walk(c)
		}
	}
	walk(n)
	out := make([]int, 0, len(seen))
	for m := range seen {
		out = append(out, m)
	}
	sort.Ints(out)
	n.subtree, n.subtreeSet = out, true
	return out
}

func (n *Node) listElems() []elem {
	if n.elemsSet {
		return n.elems
	}
	for raw, c := range n.children {
		e := elem{node: c, index: -1}
		switch {
		case strings.HasPrefix(raw, "i:"):
			i, err := strconv.Atoi(raw[2:])
			if err != nil {
				continue
			}
			e.kind, e.index = 'i', i
		case strings.HasPrefix(raw, "v:"):
			v, err := decodeJSON(raw[2:])
			if err != nil {
				continue
			}
			e.kind, e.value = 'v', v
		case strings.HasPrefix(raw, "k:"):
			v, err := decodeJSON(raw[2:])
			if err != nil {
				continue
			}
			m, ok := v.(map[string]any)
			if !ok {
				continue
			}
			e.kind, e.match = 'k', m
		default:
			continue
		}
		n.elems = append(n.elems, e)
	}
	n.elemsSet = true
	return n.elems
}

// decodeJSON decodes a fieldsV1 key fragment, keeping numbers as json.Number so
// they compare equal to the numbers decoded out of the object itself.
func decodeJSON(s string) (any, error) {
	dec := json.NewDecoder(strings.NewReader(s))
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err != nil {
		return nil, err
	}
	return v, nil
}

// Cursor points at a position in the ownership tree while the object is walked.
// The zero Cursor is valid and owns nothing.
type Cursor struct {
	node *Node
	// inherited carries the owners of an atomic ancestor down through the
	// nodes the fieldsV1 set does not mention.
	inherited []int
}

// Cursor returns a cursor on the root of the tree.
func (s *Set) Cursor() Cursor { return Cursor{node: &s.root} }

// Owners is the managers that own the node the cursor points at, either
// directly or through an atomic ancestor. It is empty for a node nobody claims
// and for a node that is only a step on the way to owned nodes.
func (c Cursor) Owners() []int {
	if c.node != nil {
		if o := c.node.ownerList(); len(o) > 0 {
			return o
		}
		return nil
	}
	return c.inherited
}

// DisplayOwners is Owners, falling back for an unowned container node to the
// single manager that owns everything below it, if there is exactly one. It is
// what a line such as "spec:" should be coloured by: the field itself belongs
// to nobody, but everything it holds has one owner.
func (c Cursor) DisplayOwners() []int {
	if o := c.Owners(); len(o) > 0 {
		return o
	}
	if c.node == nil {
		return nil
	}
	if sub := c.node.subtreeOwners(); len(sub) == 1 {
		return sub
	}
	return nil
}

// descend moves to child, which may be nil when the fieldsV1 set says nothing
// about it.
func (c Cursor) descend(child *Node) Cursor {
	if child != nil {
		return Cursor{node: child}
	}
	if c.node != nil {
		if c.node.atomic() {
			return Cursor{inherited: c.node.ownerList()}
		}
		return Cursor{}
	}
	return Cursor{inherited: c.inherited}
}

// Child moves to the value of a map key.
func (c Cursor) Child(key string) Cursor {
	var child *Node
	if c.node != nil {
		child = c.node.children["f:"+key]
	}
	return c.descend(child)
}

// Elem moves to a list element, matching it the way the fieldsV1 set names it:
// by index, by value for a set of scalars, or by key fields for an associative
// list.
func (c Cursor) Elem(index int, value any) Cursor {
	var child *Node
	if c.node != nil {
		for _, e := range c.node.listElems() {
			if e.matches(index, value) {
				child = e.node
				break
			}
		}
	}
	return c.descend(child)
}

func (e elem) matches(index int, value any) bool {
	switch e.kind {
	case 'i':
		return e.index == index
	case 'v':
		return jsonEqual(e.value, value)
	case 'k':
		m, ok := value.(map[string]any)
		if !ok {
			return false
		}
		for k, want := range e.match {
			got, ok := m[k]
			if !ok || !jsonEqual(want, got) {
				return false
			}
		}
		return true
	}
	return false
}

// jsonEqual compares two decoded JSON values. Numbers are compared by their
// canonical numeric value so that 1 and 1.0 match.
func jsonEqual(a, b any) bool {
	an, aok := a.(json.Number)
	bn, bok := b.(json.Number)
	if aok && bok {
		if af, aerr := an.Float64(); aerr == nil {
			if bf, berr := bn.Float64(); berr == nil {
				return af == bf
			}
		}
		return an == bn
	}
	switch av := a.(type) {
	case map[string]any:
		bv, ok := b.(map[string]any)
		if !ok || len(av) != len(bv) {
			return false
		}
		for k, v := range av {
			ov, ok := bv[k]
			if !ok || !jsonEqual(v, ov) {
				return false
			}
		}
		return true
	case []any:
		bv, ok := b.([]any)
		if !ok || len(av) != len(bv) {
			return false
		}
		for i := range av {
			if !jsonEqual(av[i], bv[i]) {
				return false
			}
		}
		return true
	}
	return a == b
}

// Decode reads a JSON document, keeping numbers verbatim so they can be printed
// back exactly as the API server wrote them.
func Decode(data []byte) (any, error) {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err != nil {
		return nil, err
	}
	return v, nil
}
