package graph

import (
	"encoding/json"
	"fmt"
	"github.com/RyanJarv/liquidswards/lib/utils"
	"io/fs"
	"io/ioutil"
	"sync"
)

func NewDirectedGraph[T Value]() *Graph[T] {
	return &Graph[T]{
		nodes: map[string]Node[T]{},
		m:     &sync.Mutex{},
	}
}

type Graph[T Value] struct {
	// Nodes describes all vertices contained in the graph The key will be the Node value of the connected
	// vertices with the value being the pointer to it
	nodes map[string]Node[T]
	m     *sync.Mutex
}

// The AddEdge method adds an edge between two vertices in the graph
func (g *Graph[T]) AddEdge(k1, k2 T) {
	// Avoid self-cycles
	if k1.ID() == k2.ID() {
		return
	}

	n1, err := g.getNode(k1.ID())
	if err != nil {
		n1 = g.AddNode(k1)
	}

	n2, err := g.getNode(k2.ID())
	if err != nil {
		n2 = g.AddNode(k2)
	}

	g.m.Lock()
	n1.assumes[n2.value.ID()] = n2
	n2.assumedBy[n1.value.ID()] = n1

	// Add the vertices to the graph's node map
	g.nodes[n1.value.ID()] = n1
	g.nodes[n2.value.ID()] = n2
	g.m.Unlock()
}

// DFS runs a depth first search on the graph
func (g *Graph[T]) DFS(ctx utils.Context, start string, visited map[string]bool, path []Node[T], visitCb func(Node[T], []Node[T]), last bool) {
	startNode, err := g.getNode(start)
	if err != nil {
		ctx.Error.Println(err)
		return
	}

	newVisited := make(map[string]bool)
	// we maintain a map of visited nodes to prevent visiting the same node more than once
	if visited != nil {
		newVisited = visited
	}

	// Build path to current node
	if len(path) == 0 {
		path = []Node[T]{}
	} else {
		path = append(path, startNode)
	}

	newVisited[start] = true
	visitCb(startNode, path)

	for _, v := range startNode.assumes {
		select {
		case <-ctx.Done():
			return
		default:
			if last {
				continue
			} else if newVisited[v.Value().ID()] {
				g.DFS(ctx, v.Value().ID(), newVisited, path, visitCb, true)
			} else {
				g.DFS(ctx, v.Value().ID(), newVisited, path, visitCb, false)
			}
		}
	}
}

func (g *Graph[T]) MarshalJSON() ([]byte, error) {
	return json.Marshal(g.nodes)
}

func (g *Graph[T]) UnmarshalJSON(bytes []byte) error {
	obj := map[string]*node[T]{}

	err := json.Unmarshal(bytes, &obj)
	if err != nil {
		return err
	}

	err = g.fillNodes(obj)
	if err != nil {
		return err
	}

	return err
}

// fillNodes resolves any nil values in the n.assumes and n.assumedBy fields.
func (g *Graph[T]) fillNodes(obj map[string]*node[T]) error {
	g.nodes = map[string]Node[T]{}
	for name, n := range obj {
		g.nodes[name] = n
	}

	for name, n := range obj {
		for k, _ := range n.Outbound() {
			node, err := g.GetNode(k)

			if err != nil {
				return err
			}
			n.assumes[k] = node
		}

		for k, _ := range n.Inbound() {
			node, err := g.GetNode(k)

			if err != nil {
				return err
			}
			n.assumedBy[k] = node
		}
		g.nodes[name] = n
	}
	return nil
}

func (g *Graph[T]) Load(path string) error {
	path = utils.Must(utils.ExpandPath(path))
	file, err := ioutil.ReadFile(path)
	if err != nil {
		return fmt.Errorf("Load(): %w", err)
	}

	err = json.Unmarshal(file, g)
	if err != nil {
		return fmt.Errorf("Load(): %w", err)
	}

	for _, n := range g.nodes {
		n.Value().SetGraph(g)
	}
	return nil
}

func (g *Graph[T]) Save(path string) error {
	bytes, err := g.MarshalJSON()
	if err != nil {
		return fmt.Errorf("Error(): %w", err)
	}
	return ioutil.WriteFile(path, bytes, fs.FileMode(0o600))
}
