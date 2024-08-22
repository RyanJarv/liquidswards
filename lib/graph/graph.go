package graph

import (
	"bytes"
	"encoding/json"
	"fmt"
	"github.com/RyanJarv/liquidswards/lib/utils"
	"github.com/goccy/go-graphviz"
	"github.com/goccy/go-graphviz/cgraph"
	"io/fs"
	"log"
	"os"
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
	// vertice with the value being the pointer to it
	nodes map[string]Node[T]
	m     *sync.Mutex
}

// The AddEdge method adds an edge between two vertices in the graph
func (g *Graph[T]) AddEdge(k1, k2 T) {
	n1, ok := g.getNode(k1.Id())
	if !ok {
		n1 = g.AddNode(k1)
	}

	n2, ok := g.getNode(k2.Id())
	if !ok {
		n2 = g.AddNode(k2)
	}

	g.m.Lock()
	n1.assumes[n2.value.Id()] = n2
	n2.assumedBy[n1.value.Id()] = n1

	// Add the vertices to the graph's node map
	g.nodes[n1.value.Id()] = n1
	g.nodes[n2.value.Id()] = n2
	g.m.Unlock()
}

// DFS runs a depth first search on the graph
func (g *Graph[T]) DFS(ctx utils.Context, start string, visited map[string]bool, path []Node[T], visitCb func(Node[T], []Node[T]), last bool) {
	startNode, ok := g.getNode(start)
	if !ok {
		ctx.Error.Printf("DFS(): the graph node with key '%v' does not exist", start)
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
			} else if newVisited[v.Value().Id()] {
				g.DFS(ctx, v.Value().Id(), newVisited, path, visitCb, true)
			} else {
				g.DFS(ctx, v.Value().Id(), newVisited, path, visitCb, false)
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
			node, ok := g.GetNode(k)
			if !ok {
				return fmt.Errorf("fillNodes(): the graph node with key '%v' does not exist\n", k)
			}
			n.assumes[k] = node
		}

		for k, _ := range n.Inbound() {
			node, ok := g.GetNode(k)
			if !ok {
				return fmt.Errorf("fillNodes(): the graph node with key '%v' does not exist\n", k)
			}
			n.assumedBy[k] = node
		}
		g.nodes[name] = n
	}
	return nil
}

func (g *Graph[T]) Load(path string) error {
	path = utils.Must(utils.ExpandPath(path))
	file, err := os.ReadFile(path)
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
	b, err := g.MarshalJSON()
	if err != nil {
		return fmt.Errorf("Error(): %w", err)
	}
	return os.WriteFile(path, b, fs.FileMode(0o600))
}

func (g *Graph[T]) PrintGraph(ctx utils.Context, nodes []T) error {
	fmt.Println(utils.Green.Color("\nAccessed:"))
	for _, cfg := range nodes {
		g.DFS(ctx, cfg.Id(), nil, []Node[T]{}, func(node Node[T], path []Node[T]) {
			fmt.Printf("\n")
			for i := 0; i < len(path); i++ {
				fmt.Printf("\t")
			}
			if len(path) == 0 {
				fmt.Printf(" "+utils.Cyan.Color("*")+" %s", node.Value().Id())
			} else {
				fmt.Printf(utils.Cyan.Color("->")+" %s", node.Value().Id())
			}
		}, false)
	}
	fmt.Printf("\n")
	return nil
}

func (g *Graph[T]) SaveDiagram(ctx utils.Context, nodes []T, path string) error {
	graph := graphviz.New()
	gviz, err := graph.Graph()
	if err != nil {
		log.Fatal(err)
	}
	defer func() {
		if err := gviz.Close(); err != nil {
			log.Fatal(err)
		}
		utils.Must0(graph.Close())
	}()
	gviz.SetRankDir("LR")

	color := utils.ColorFromArn()

	fmt.Println(utils.Green.Color("\nGraphViz:"))
	for _, cfg := range nodes {
		conv := map[string]*cgraph.Node{}

		g.DFS(ctx, cfg.Id(), nil, []Node[T]{}, func(node Node[T], path []Node[T]) {
			n1, ok := g.GetNode(node.Value().Id())
			if !ok {
				ctx.Error.Println("SaveDiagram(): the graph node with key '%v' does not exist\n", node.Value().Id())
				return
			}

			g1, ok := conv[n1.Value().Id()]
			if !ok {
				g1, err = gviz.CreateNode(n1.Value().Id())
				if err != nil {
					log.Fatal(err)
				}
				g1.SetColor(color.Get(n1.Value().Id()))
				g1.SetStyle("filled")
				conv[n1.Value().Id()] = g1
			}

			for _, edge := range n1.Outbound() {
				n2Id := edge.Value().Id()

				g2, ok := conv[n2Id]
				if !ok {
					g2, err = gviz.CreateNode(n2Id)
					if err != nil {
						log.Fatal(err)
					}
					g2.SetColor(color.Get(n2Id))
					g2.SetStyle("filled")
					conv[n2Id] = g2
				}

				e1, err := gviz.CreateEdge(fmt.Sprintf("%s-%s", n1.Value().Id(), n2Id), g1, g2)
				if err != nil {
					log.Fatal(err)
				}
				e1.SetDir("forward")
			}

		}, false)
	}

	var buf bytes.Buffer
	if err := graph.Render(gviz, "dot", &buf); err != nil {
		log.Fatal(err)
	}

	err = os.WriteFile(path, buf.Bytes(), fs.FileMode(0640))
	if err != nil {
		return fmt.Errorf("failed writing graphviz output to %s: %w", path, err)
	}
	return nil
}

func (g *Graph[T]) Report(ctx utils.Context, nodes []T, path string) error {
	err := g.PrintGraph(ctx, nodes)
	if err != nil {
		return fmt.Errorf("printing results: %w", err)
	}

	err = g.SaveDiagram(ctx, nodes, path)
	if err != nil {
		ctx.Error.Printf("generating graphviz Graph failed: %l\n", err)
	}
	return nil
}
