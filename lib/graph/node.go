package graph

import (
	"encoding/json"
	"fmt"
)

type Value interface {
	json.Marshaler
	ID() string
	SetGraph(graph interface{})
}

type Node[T Value] interface {
	json.Marshaler
	json.Unmarshaler
	Value() T
	Outbound() map[string]Node[T]
	Inbound() map[string]Node[T]
}

type node[T Value] struct {
	// value is the unique identifier of the vertex
	value T `json:"Value"`

	// assumes will describe vertices connected to this one The key will be the Node value of the connected
	// vertices with the value being the pointer to it
	assumes map[string]Node[T] `json:"Assumes"`

	// assumedBy stores references to other roles that can assume this role. This is useful if you want to determine
	// the path needed to access a specific role.
	assumedBy map[string]Node[T] `json:"AssumedBy"`
}

type NewNodeInput[T Value] struct {
	Value     T
	Assumes   []Node[T]
	AssumedBy []Node[T]
}

func NewNode[T Value](in NewNodeInput[T]) Node[T] {
	// TODO: Check if we can just export node, can't remember but I think I remember this causing issues.
	node := &node[T]{
		value:     in.Value,
		assumes:   map[string]Node[T]{},
		assumedBy: map[string]Node[T]{},
	}
	for _, n := range in.Assumes {
		node.assumes[n.Value().ID()] = n
	}
	for _, n := range in.AssumedBy {
		node.assumedBy[n.Value().ID()] = n
	}
	return node
}

// Outbound returns a map of nodes that are connected by outbound edges.
func (n *node[T]) Outbound() map[string]Node[T] {
	return n.assumes
}

// Inbound returns a map of nodes that are connected by inbound edges.
func (n *node[T]) Inbound() map[string]Node[T] {
	return n.assumedBy
}

// Value returns the original value passed to Graph.AddNode()
func (n *node[T]) Value() T { return n.value }

// Nodes returns the current map of nodes in the graph.
func (g *Graph[T]) Nodes() map[string]Node[T] {
	g.m.Lock()
	defer g.m.Unlock()
	return g.nodes
}

// AddNode adds a new node with the given key to the graph if it doesn't already exist.
//
// The method returns a bool indicating whether the node with the given key is new. If it is new, true is returned, if
// it is a duplicate false is returned.
func (g *Graph[T]) AddNode(n T) *node[T] {
	if _, ok := g.nodes[n.ID()]; ok {
		return nil
	}

	v := &node[T]{
		value:     n,
		assumes:   map[string]Node[T]{},
		assumedBy: map[string]Node[T]{},
	}

	g.m.Lock()
	g.nodes[n.ID()] = v
	g.m.Unlock()

	return v
}

// GetNode returns the Node represented by type T
func (g *Graph[T]) GetNode(k string) (Node[T], error) {
	g.m.Lock()
	defer g.m.Unlock()

	node, ok := g.nodes[k]
	if !ok {
		return node, fmt.Errorf("the graph node with key '%v' does not exist\n", k)
	}

	return node, nil
}

// getNode returns the node (the struct, not the interface) represented by type T
func (g *Graph[T]) getNode(k string) (*node[T], error) {
	n, err := g.GetNode(k)
	return n.(*node[T]), err
}

type JsonNode[T Value] struct {
	Value     T        `json:"Value"`
	Assumes   []string `json:"Assumes"`
	AssumedBy []string `json:"AssumedBy"`
}

func (n *node[T]) MarshalJSON() ([]byte, error) {
	obj := JsonNode[T]{
		Value: n.value,
	}
	for k, _ := range n.assumes {
		obj.Assumes = append(obj.AssumedBy, k)
	}
	for k, _ := range n.assumedBy {
		obj.AssumedBy = append(obj.AssumedBy, k)
	}
	return json.Marshal(obj)
}

// UnmarshalJSON un-marshals constructs a node struct from a JsonNode bytes string.
// This is a bit of a hack because the json string will only contain references to nodes in the Outbound and Inbound
// fields, and we don't have a reference to the graph to resolve them. To work around this we just set the value in the
// node.assumes/node.assumedBy map to nil using the correct key. The Graph.UnmarshalJSON function then handles resolving
// the nil values by looking up them up by the key.
func (n *node[T]) UnmarshalJSON(bytes []byte) error {
	obj := JsonNode[T]{Value: n.value}
	err := json.Unmarshal(bytes, &obj)
	if err != nil {
		return err
	}

	n.value = obj.Value
	n.assumes = initMap[T](obj.Assumes)
	n.assumedBy = initMap[T](obj.AssumedBy)

	return nil
}

func initMap[T Value](obj []string) map[string]Node[T] {
	m := map[string]Node[T]{}
	for _, v := range obj {
		m[v] = nil
	}
	return m
}
