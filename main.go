package main

import (
	"context"
	"flag"
	"fmt"
	"github.com/RyanJarv/lq"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials/stscreds"
	"github.com/aws/aws-sdk-go-v2/service/iam"
	"github.com/aws/aws-sdk-go-v2/service/iam/types"
	"github.com/aws/aws-sdk-go-v2/service/sts"
	"log"
	"os"
	"strings"
)

type Color string

const (
	Red    Color = "\033[31m"
	Green  Color = "\033[32m"
	Yellow Color = "\033[33m"
	Blue   Color = "\033[34m"
	Purple Color = "\033[35m"
	Cyan   Color = "\033[36m"
	Gray   Color = "\033[37m"
	White  Color = "\033[97m"
)

func (c Color) Color(s ...string) string {
	return string(c) + strings.Join(s, " ") + "\033[0m"
}

type L struct {
	Info  *log.Logger
	Debug *log.Logger
	Error *log.Logger
}

var (
	region  = flag.String("region", "", "The AWS Region to use")
	profile = flag.String("profile", "", "The AWS Profile to use")
	debug   = flag.Bool("debug", false, "Enable debug output")
	l       = L{
		Info:  log.New(os.Stdout, Green.Color("[INFO] "), 0),
		Debug: log.New(os.Stderr, Gray.Color("[DEBUG] "), 0),
		Error: log.New(os.Stderr, Red.Color("[ERROR] "), 0),
	}
)

func main() {
	flag.Parse()
	inScope := flag.Args()

	if !*debug {
		null, err := os.Open(os.DevNull)
		if err != nil {
			l.Error.Fatalln(err)
		}
		l.Debug.SetOutput(null)
	}

	l.Debug.Printf("using region %s\n", *region)

	err := Run(context.Background(), inScope, region, profile)
	if err != nil {
		l.Error.Fatalln(err)
	}
}

func Run(ctx context.Context, scope []string, region *string, profile *string) error {
	cfg, err := config.LoadDefaultConfig(ctx, config.WithRegion(*region), config.WithSharedConfigProfile(*profile))
	if err != nil {
		return fmt.Errorf("failed loading profile %s using regin %s: %w", *profile, *region, err)
	}

	roles := NewRoles(ctx)
	roles.Run(&cfg)
	roles.q.Wait()

	roles.PrintGraph()

	return nil
}

func NewVertex[T comparable](key T) *Vertex[T] {
	return &Vertex[T]{
		Key:   key,
		Nodes: map[T]*Vertex[T]{},
	}
}

type Vertex[T comparable] struct {
	// Key is the unique identifier of the vertex
	Key T
	// Nodes will describe vertices connected to this one The key will be the Key value of the connected
	// vertice with the value being the pointer to it
	Nodes map[T]*Vertex[T]
}

func NewDirectedGraph[T comparable]() *Graph[T] {
	return &Graph[T]{Nodes: map[T]*Vertex[T]{}}
}

type Graph[T comparable] struct {
	// Nodes describes all vertices contained in the graph The key will be the Key value of the connected
	// vertice with the value being the pointer to it
	Nodes map[T]*Vertex[T]
}

// AddNode creates a new vertex with the given key if it doesn't already exist and adds it to the graph
func (g *Graph[T]) AddNode(key T) {
	if _, ok := g.Nodes[key]; ok {
		return
	}
	v := NewVertex(key)
	g.Nodes[key] = v
}

// The AddEdge method adds an edge between two vertices in the graph
func (g *Graph[T]) AddEdge(k1, k2 T) {
	v1 := g.Nodes[k1]
	v2 := g.Nodes[k2]

	// return an error if one of the vertices doesn't exist
	if v1 == nil || v2 == nil {
		l.Error.Fatalln("not all vertices exist")
	}

	// do nothing if the vertices are already connected
	if _, ok := v1.Nodes[v2.Key]; ok {
		return
	}

	v1.Nodes[v2.Key] = v2

	// Add the vertices to the graph's vertex map
	g.Nodes[v1.Key] = v1
	g.Nodes[v2.Key] = v2
}

// here, we import the graph we defined in the previous section as the `graph` package
func DFS[T comparable](g *Graph[T], startNode *Vertex[T], visited *map[T]bool, depth int, visitCb func(T, int)) {
	// we maintain a map of visited nodes to prevent visiting the same node more than once
	if visited == nil {
		visited = &map[T]bool{}
	}

	if startNode == nil {
		return
	}
	//visited[startNode.Key] = true
	visitCb(startNode.Key, depth)

	depth += 1
	// for each of the adjacent vertices, call the function recursively if it hasn't yet been visited
	for _, v := range startNode.Nodes {
		if (*visited)[v.Key] {
			continue
		}
		DFS[T](g, v, visited, depth, visitCb)
	}
}

//func NewRoleGraph() {
//	RoleGraph{
//		graph: graph.New(math.MaxInt),
//	}
//}
//
//type RoleGraph struct {
//	nodes []*string
//	edges map[string][]*string
//	lock  sync.RWMutex
//	graph *graph.Mutable
//}
//
//// AddNode adds a node to the graph
//func (g *RoleGraph) AddNode(n *string) {
//	g.lock.Lock()
//	g.nodes = append(g.nodes, n)
//	g.lock.Unlock()
//}
//
//func (g *RoleGraph) GetNode(n string) *string {
//	for _, v := range g.nodes {
//		if n == *v {
//			return v
//		}
//	}
//	return nil
//}
//
//// AddEdge adds an edge to the graph
//func (g *RoleGraph) AddEdge(n1, n2 *string) {
//	g.lock.Lock()
//	if g.edges == nil {
//		g.edges = make(map[string][]*string)
//	}
//	g.edges[*n1] = append(g.edges[*n1], n2)
//	g.lock.Unlock()
//}
//
//// AddEdge adds an edge to the graph
//func (g *RoleGraph) Print() {
//	g.lock.RLock()
//	for _, v := range g.nodes {
//		curr := []string{*v}
//		i := 0
//		for down := g.edges[curr[len(curr)]]; len(down) > 0 {
//			curr = append(curr, *g.edges[curr][0])
//		}
//		for _, j := range g.edges[*v] {
//			fmt.Printf("\n%s -> \n", v)
//		}
//	}
//	g.lock.RUnlock()
//}
//
//func (g *RoleGraph) nearNode() {
//	return g.edges[n]
//}

//func EnumerateRoles(ctx context.Context, cfg *aws.Config, scope []string) *sync.WaitGroup {
//	return nil
//}
//
func NewRoles(ctx context.Context) *Roles {
	return &Roles{
		q:   lq.NewListQueue[types.Role](),
		ctx: ctx,
	}
}

type Roles struct {
	q          *lq.ListQueue[types.Role]
	ctx        context.Context
	discovered *Graph[string]
	accessed   *Graph[string]
}

func (r *Roles) Run(cfg *aws.Config) {
	r.discovered = NewDirectedGraph[string]()
	r.accessed = NewDirectedGraph[string]()

	r.run(cfg, nil)
}

func (r *Roles) run(cfg *aws.Config, prevIdentity *string) {
	svc := sts.NewFromConfig(*cfg)
	resp, err := svc.GetCallerIdentity(r.ctx, &sts.GetCallerIdentityInput{})
	if err != nil {
		l.Error.Fatalln(err)
	}

	currArn := CleanArn(*resp.Arn)

	r.accessed.AddNode(currArn)
	r.discovered.AddNode(currArn)

	if prevIdentity != nil {
		r.accessed.AddEdge(*prevIdentity, currArn)
	}

	r.asssumeAllRoles(cfg, currArn)
	r.listRoles(cfg, currArn)
}

func CleanArn(arn string) string {
	if strings.Contains(arn, "assumed-role") {
		parts := strings.Split(arn, "/")
		parts[2] = "role"
		arn = strings.Join(parts[0:len(parts)-1], "/")
	}
	return arn
}

func (r *Roles) asssumeAllRoles(cfg *aws.Config, identity string) {
	svc := sts.NewFromConfig(*cfg)
	roleCh := r.q.Each()

	go func() {
		for role := range roleCh {
			l.Debug.Printf("attempting to assume role %s\n", role)
			// TODO: If this succeeds it ends up calling sts:AssumeRole twice as much as necessary.
			_, err := svc.AssumeRole(r.ctx, &sts.AssumeRoleInput{
				RoleArn:         role.Arn,
				RoleSessionName: aws.String("liquidswards-assumerole-test"),
				DurationSeconds: aws.Int32(900),
				ExternalId:      nil, // TODO: pass external ID along with role ARN
			})

			if err != nil {
				l.Debug.Println(err.Error())
			} else {
				l.Info.Printf("%s "+Cyan.Color("--assumed role--> ")+" %s", identity, *role.Arn)

				newCfg := *cfg
				newCfg.Credentials = stscreds.NewAssumeRoleProvider(sts.NewFromConfig(*cfg), *role.Arn)
				r.run(&newCfg, &identity)
			}
			r.q.Done()
		}
	}()
}

func (r *Roles) listRoles(cfg *aws.Config, identity string) {
	svc := iam.NewFromConfig(*cfg)

	resp := &iam.ListRolesOutput{IsTruncated: true}

	var err error
	for err == nil && resp.IsTruncated {
		resp, err = svc.ListRoles(r.ctx, &iam.ListRolesInput{})
		if err != nil {
			l.Debug.Println(err)
		} else {
			for _, v := range resp.Roles {
				l.Debug.Printf("%s found role %s\n", identity, *v.Arn)
				r.discovered.AddNode(*v.Arn)
				r.discovered.AddEdge(identity, *v.Arn)
			}
			r.q.Add(resp.Roles...)
		}
	}
}

func (r *Roles) PrintGraph() {
	fmt.Println(Green.Color("\nAccessed:"))
	for _, start := range r.accessed.Nodes {
		if len(start.Nodes) > 0 {
			DFS[string](r.accessed, start, nil, 0, func(node string, depth int) {
				fmt.Printf("\n")
				for i := 0; i < depth; i++ {
					fmt.Printf("\t")
				}
				if depth == 0 {
					fmt.Printf(" "+Cyan.Color("*")+" %s", node)
				} else {
					fmt.Printf(Cyan.Color("->")+" %s", node)
				}
			})
		}
	}
	fmt.Printf("\n")
}
