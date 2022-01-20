package graph

import (
	"fmt"
	"github.com/RyanJarv/liquidswards/lib/utils"
	"log"
	"sync"
)

func NewNode[T comparable](key T) *Node[T] {
	return &Node[T]{
		Key:   key,
		Edges: map[T]*Edge[T]{},
		m:     &sync.Mutex{},
	}
}

type Edge[T comparable] struct {
	Node T

	// Accessed indicates if this edge represents successful access or not. This is useful to avoid retrying failed
	// access attempts.
	Accessed   *bool
	Discovered *bool
	m          *sync.Mutex
}

type Node[T comparable] struct {
	// Key is the unique identifier of the vertex
	Key T
	// Edges will describe vertices connected to this one The key will be the Key value of the connected
	// vertice with the value being the pointer to it
	Edges map[T]*Edge[T]
	m     *sync.Mutex
}

func NewDirectedGraph[T comparable]() *Graph[T] {
	return &Graph[T]{
		Nodes: map[T]*Node[T]{},
		m:     &sync.Mutex{},
	}
}

type Graph[T comparable] struct {
	// Nodes describes all vertices contained in the graph The key will be the Key value of the connected
	// vertice with the value being the pointer to it
	Nodes map[T]*Node[T]
	m     *sync.Mutex
}

// AddNode adds a new node with the given key to the graph if it doesn't already exist.
//
// The method returns a bool indicating whether the node with the given key is new. If it is new, true is returned, if
// it is a duplicate false is returned.
func (g *Graph[T]) AddNode(key T) bool {
	if _, ok := g.Nodes[key]; ok {
		return false
	}

	v := NewNode(key)

	g.m.Lock()
	g.Nodes[key] = v
	g.m.Unlock()

	return true
}

func (g *Graph[T]) Discovered(k1, k2 T) *bool {
	v1, err1 := g.GetNode(k1)
	v2, err2 := g.GetNode(k2)

	// return an error if one of the vertices doesn't exist
	if err1 != nil || err2 != nil {
		return nil
	}

	v1.m.Lock()
	defer v1.m.Unlock()
	return v1.Edges[v2.Key].Discovered
}

func (g *Graph[T]) Accessed(k1, k2 T) *bool {
	v1, err1 := g.GetNode(k1)
	v2, err2 := g.GetNode(k2)

	// return an error if one of the vertices doesn't exist
	if err1 != nil || err2 != nil {
		log.Fatalln(utils.Colesce(err1, err2))
	}

	v1.m.Lock()
	defer v1.m.Unlock()
	return v1.Edges[v2.Key].Accessed
}

// The AddEdge method adds an edge between two vertices in the graph
func (g *Graph[T]) AddEdge(k1, k2 T, accessed *bool, discovered *bool) {
	v1, err := g.GetNode(k1)
	if err != nil {
		v1 = NewNode(k1)
	}

	v2, err := g.GetNode(k2)
	if err != nil {
		v2 = NewNode(k2)
	}

	edge := &Edge[T]{
		Node: v2.Key,
		m:    &sync.Mutex{},
	}

	var oldAccess *bool
	var oldDiscovered *bool

	v1.m.Lock()
	oldEdge, ok := v1.Edges[v2.Key]
	if ok {
		oldAccess = oldEdge.Accessed
		oldDiscovered = oldEdge.Discovered
	}
	v1.m.Unlock()

	edge.Accessed = utils.Colesce(accessed, oldAccess).(*bool)
	edge.Discovered = utils.Colesce(discovered, oldDiscovered).(*bool)

	v1.m.Lock()
	v1.Edges[v2.Key] = edge
	v1.m.Unlock()

	// Add the vertices to the graph's node map
	g.m.Lock()
	g.Nodes[v1.Key] = v1
	g.Nodes[v2.Key] = v2
	g.m.Unlock()
}

func (g *Graph[T]) HasNode(k T) bool {
	g.m.Lock()
	defer g.m.Unlock()

	if _, ok := g.Nodes[k]; ok {
		return true
	} else {
		return false
	}
}

func (g *Graph[T]) GetNode(k T) (*Node[T], error) {
	g.m.Lock()
	defer g.m.Unlock()

	if node, ok := g.Nodes[k]; ok {
		return node, nil
	} else {
		return nil, fmt.Errorf("the graph node with key '%v' does not exist\n", k)
	}
}

func (g *Graph[T]) HasEdge(k1, k2 T) bool {
	v1, err1 := g.GetNode(k1)
	v2, err2 := g.GetNode(k2)
	if err1 != nil || err2 != nil {
		return false
	}

	v1.m.Lock()
	defer v1.m.Unlock()
	for _, edge := range v1.Edges {
		if edge.Node == v2.Key {
			return true
		}
	}
	return false
}

// here, we import the graph we defined in the previous section as the `graph` package
func DFS[T comparable](g *Graph[T], startNode *T, accept func(*Edge[T]) bool, visited map[T]bool, path []T, visitCb func(T, []T), last bool) {
	if startNode == nil {
		return
	}

	newVisited := make(map[T]bool)
	// we maintain a map of visited nodes to prevent visiting the same node more than once
	if visited != nil {
		newVisited = visited
	}

	// Build path to current node
	if len(path) == 0 {
		path = []T{*startNode}
	} else {
		path = append(path, *startNode)
	}

	newVisited[*startNode] = true
	visitCb(*startNode, path)

	// for each of the adjacent vertices, call the function recursively if it hasn't yet been visited
	node, err := g.GetNode(*startNode)
	if err != nil {
		log.Fatalln(err)
	}

	for _, v := range node.Edges {
		if !accept(v) {
			continue
		}

		if last {
			continue
		} else if newVisited[v.Node] {
			DFS[T](g, &v.Node, accept, newVisited, path, visitCb, true)
		} else {
			DFS[T](g, &v.Node, accept, newVisited, path, visitCb, false)
		}
	}
}
