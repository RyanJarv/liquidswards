package lib

import (
	"bytes"
	"context"
	"fmt"
	"github.com/RyanJarv/liquidswards/lib/creds"
	"github.com/RyanJarv/liquidswards/lib/graph"
	"github.com/RyanJarv/liquidswards/lib/plugins"
	"github.com/RyanJarv/liquidswards/lib/utils"
	"github.com/alitto/pond"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/iam"
	"github.com/aws/aws-sdk-go-v2/service/iam/types"
	"github.com/dlsniper/debugger"
	"github.com/goccy/go-graphviz"
	"github.com/goccy/go-graphviz/cgraph"
	"io/fs"
	"io/ioutil"
	"log"
	"strings"
	"sync"
)

type Role struct {
	Arn        string
	ExternalId interface{}
}

func (ls *AssumeRoles) Report(ctx utils.Context, cfgs []*creds.Config, path string) error {
	err := ls.PrintGraph(ctx, cfgs)
	if err != nil {
		return fmt.Errorf("printing results: %w", err)
	}

	err = ls.SaveDiagram(ctx, cfgs, path)
	if err != nil {
		ctx.Error.Printf("generating graphviz Graph failed: %l\n", err)
	}
	return nil
}

func AssumeAllRoles(global plugins.GlobalPluginArgs, pool *pond.WorkerPool, plugins []plugins.DiscoveryPlugin) *AssumeRoles {
	return &AssumeRoles{
		GlobalPluginArgs: global,
		pool:             pool,
		Plugins:          plugins,
		m:                &sync.Mutex{},
	}
}

type AssumeRoles struct {
	plugins.GlobalPluginArgs
	ctx     utils.Context
	m       *sync.Mutex
	pool    *pond.WorkerPool
	Plugins []plugins.DiscoveryPlugin
}

func FindRoles(ctx context.Context, cfg aws.Config, region string, scope []string) ([]types.Role, error) {
	svc := iam.NewFromConfig(cfg, func(opts *iam.Options) {
		opts.Region = region
	})

	roles := []types.Role{}
	paginator := iam.NewListRolesPaginator(svc, &iam.ListRolesInput{})
	for paginator.HasMorePages() {
		resp, err := paginator.NextPage(ctx)
		if err != nil {
			return nil, fmt.Errorf("failed to retrieve roles: %w", err)
		}
		for _, role := range resp.Roles {
			if scope != nil && !utils.ArnInScope(scope, *role.Arn) {
				continue
			}
			roles = append(roles, role)
		}
	}
	return roles, nil
}

func (a *AssumeRoles) Scan(ctx utils.Context, cfgs []*creds.Config) error {
	for _, cfg := range cfgs {
		ch := a.Lq.Each()

		a.Graph.AddNode(cfg)
		a.FindRoles(ctx, cfg, cfg.Arn())

		for _, plugin := range a.Plugins {
			plugin.Run(ctx, cfg)
		}

		a.pool.Submit(func() {
			debugger.SetLabels(func() []string {
				return []string{"assumeRoles", cfg.Arn()}
			})
			a.assumeRoles(ctx, cfg, []string{cfg.Arn()}, ch)
		})
	}
	return nil
}

func (a *AssumeRoles) assumeRoles(ctx utils.Context, cfg *creds.Config, identity []string, ch chan types.Role) {
	ctx.Debug.Printf("running scan on %s", strings.Join(identity, " -> "))
	currArn := identity[len(identity)-1]
	if len(identity) == 50 {
		ctx.Info.Printf("max depth of 50 reached when enumerating %s\n", currArn)
		return
	}

	for role := range ch {
		if ctx.IsDone("Finished assuming roles, exiting...") {
			// TODO: Need to let lq know we're done here.
			return
		}

		// Avoid trying to assume our own role
		if cfg.Arn() == *role.Arn {
			return
		}

		if a.Scope != nil && !utils.ArnInScope(a.Scope, *role.Arn) {
			continue
		}

		newCfg, err := cfg.Assume(ctx, *role.Arn)
		if err != nil {
			ctx.Debug.Println(err)
			continue
		}

		newNode := a.Graph.AddNode(newCfg)
		a.Graph.AddEdge(cfg, newCfg)
		ctx.Info.Printf("%s"+utils.Arrow+"%s", strings.Join(identity, utils.Arrow), *role.Arn)

		if newNode != nil {
			a.FindRoles(ctx, cfg, *role.Arn)

			for _, plugin := range a.Plugins {
				plugin.Run(ctx, newCfg)
			}

			ch := a.Lq.Each()

			// Double func so local variables get copied.
			a.pool.Submit(func(newCfg *creds.Config, identity []string, ch chan types.Role) func() {
				return func() {
					a.assumeRoles(ctx, newCfg, identity, ch)
				}
			}(newCfg, append(identity, *role.Arn), ch))
		}
	}
}

func (a *AssumeRoles) FindRoles(ctx utils.Context, cfg *creds.Config, currArn string) {
	a.Lq.Wg.Add(1)
	a.pool.Submit(func() {
		debugger.SetLabels(func() []string {
			return []string{
				"plugins", "findRoles",
				"identity", currArn,
			}
		})
		roles, err := FindRoles(ctx, cfg.Config(), a.Region, a.Scope)
		if err != nil {
			ctx.Debug.Printf("error using role %s: %s\n", currArn, err)
		}
		for _, role := range roles {
			a.Lq.AddUnique(*role.Arn, role)
		}
		a.Lq.Wg.Done()
	})
}

func (a *AssumeRoles) PrintGraph(ctx utils.Context, cfgs []*creds.Config) error {
	fmt.Println(utils.Green.Color("\nAccessed:"))
	for _, cfg := range cfgs {
		a.Graph.DFS(ctx, cfg.ID(), nil, []graph.Node[*creds.Config]{}, func(node graph.Node[*creds.Config], path []graph.Node[*creds.Config]) {
			fmt.Printf("\n")
			for i := 0; i < len(path); i++ {
				fmt.Printf("\t")
			}
			if len(path) == 0 {
				fmt.Printf(" "+utils.Cyan.Color("*")+" %s", node.Value().Arn())
			} else {
				fmt.Printf(utils.Cyan.Color("->")+" %s", node.Value().Arn())
			}
		}, false)
	}
	fmt.Printf("\n")
	return nil
}

func (a *AssumeRoles) SaveDiagram(ctx utils.Context, cfgs []*creds.Config, path string) error {
	g := graphviz.New()
	gviz, err := g.Graph()
	if err != nil {
		log.Fatal(err)
	}
	defer func() {
		if err := gviz.Close(); err != nil {
			log.Fatal(err)
		}
		g.Close()
	}()
	gviz.SetRankDir("LR")

	color := utils.ColorFromArn()

	fmt.Println(utils.Green.Color("\nGraphViz:"))
	for _, cfg := range cfgs {
		conv := map[string]*cgraph.Node{}

		a.Graph.DFS(ctx, cfg.ID(), nil, []graph.Node[*creds.Config]{}, func(node graph.Node[*creds.Config], path []graph.Node[*creds.Config]) {
			n1, err := a.Graph.GetNode(node.Value().ID())
			if err != nil {
				ctx.Error.Println(err)
				return
			}

			g1, ok := conv[n1.Value().ID()]
			if !ok {
				g1, err = gviz.CreateNode(n1.Value().ID())
				if err != nil {
					log.Fatal(err)
				}
				g1.SetColor(color.Get(n1.Value().ID()))
				g1.SetStyle("filled")
				conv[n1.Value().ID()] = g1
			}

			for _, edge := range n1.Outbound() {
				n2Id := edge.Value().ID()

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

				e1, err := gviz.CreateEdge(fmt.Sprintf("%s-%s", n1.Value().ID(), n2Id), g1, g2)
				if err != nil {
					log.Fatal(err)
				}
				e1.SetDir("forward")
			}

		}, false)
	}

	var buf bytes.Buffer
	if err := g.Render(gviz, "dot", &buf); err != nil {
		log.Fatal(err)
	}

	err = ioutil.WriteFile(path, buf.Bytes(), fs.FileMode(0640))
	if err != nil {
		return fmt.Errorf("failed writing graphviz output to %s: %w", path, err)
	}
	return nil
}

// NewGraph exists because if a generic with two type arguments
func NewGraph() *graph.Graph[*creds.Config] {
	graph := graph.NewDirectedGraph[*creds.Config]()
	return graph
}
