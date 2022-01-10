package main

import (
	"context"
	"flag"
	"fmt"
	"github.com/RyanJarv/ListQueue"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials/stscreds"
	"github.com/aws/aws-sdk-go-v2/service/iam"
	"github.com/aws/aws-sdk-go-v2/service/iam/types"
	"github.com/aws/aws-sdk-go-v2/service/sts"
	"log"
	"os"
	"sync"
)

type L struct {
	Info  *log.Logger
	Debug *log.Logger
}

var (
	region  = flag.String("region", "", "The AWS Region to use")
	profile = flag.String("profile", "", "The AWS Profile to use")
	debug   = flag.Bool("debug", false, "Enable debug output")
	l       = L{
		Info:  log.New(os.Stdout, "[INFO] ", 0),
		Debug: log.New(os.Stderr, "[DEBUG] ", 0),
	}
)

func main() {
	flag.Parse()
	inScope := flag.Args()

	if !*debug {
		null, err := os.Open(os.DevNull)
		if err != nil {
			log.Fatalln(err)
		}
		l.Debug.SetOutput(null)
	}

	l.Debug.Printf("using region %s\n", *region)

	err := Run(context.Background(), inScope, region, profile)
	if err != nil {
		log.Fatalln(err)
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

	return nil
}

type Role struct {
	Arn string
}

type RoleGraph struct {
	nodes []*Role
	edges map[Role][]*Role
	lock  sync.RWMutex
}

// AddNode adds a node to the graph
func (g *RoleGraph) AddNode(n *Role) {
	g.lock.Lock()
	g.nodes = append(g.nodes, n)
	g.lock.Unlock()
}

// AddEdge adds an edge to the graph
func (g *RoleGraph) AddEdge(n1, n2 *Role) {
	g.lock.Lock()
	if g.edges == nil {
		g.edges = make(map[Role][]*Role)
	}
	g.edges[*n1] = append(g.edges[*n1], n2)
	g.edges[*n2] = append(g.edges[*n2], n1)
	g.lock.Unlock()
}

// AddEdge adds an edge to the graph
func (g *RoleGraph) String() {
	g.lock.RLock()
	s := ""
	for i := 0; i < len(g.nodes); i++ {
		s += g.nodes[i].Arn + " -> "
		near := g.edges[*g.nodes[i]]
		for j := 0; j < len(near); j++ {
			s += near[j].Arn + " "
		}
		s += "\n"
	}
	fmt.Println(s)
	g.lock.RUnlock()
}

func EnumerateRoles(ctx context.Context, cfg *aws.Config, scope []string) *sync.WaitGroup {
	return nil
}

func NewRoles(ctx context.Context) *Roles {
	return &Roles{
		q:   listQueue.NewListQueue[types.Role](),
		ctx: ctx,
	}
}

type Roles struct {
	q   *listQueue.ListQueue[types.Role]
	ctx context.Context
}

func (r *Roles) Run(cfg *aws.Config) {
	r.asssumeAllRoles(cfg)
	r.listRoles(cfg)
}

func (r *Roles) asssumeAllRoles(cfg *aws.Config) {
	svc := sts.NewFromConfig(*cfg)
	roleCh := r.q.Each()

	go func() {
		for role := range roleCh {
			l.Debug.Printf("Attempting to assume role %s\n", role)
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
				l.Info.Printf("Assumed role %s\n", *role.Arn)

				newCfg := *cfg
				newCfg.Credentials = stscreds.NewAssumeRoleProvider(sts.NewFromConfig(*cfg), *role.Arn)
				r.Run(&newCfg)
			}
			r.q.Done()
		}
	}()
}

func (r *Roles) listRoles(cfg *aws.Config) {
	svc := iam.NewFromConfig(*cfg)

	resp := &iam.ListRolesOutput{IsTruncated: true}

	var err error
	for err == nil && resp.IsTruncated {
		resp, err = svc.ListRoles(r.ctx, &iam.ListRolesInput{})
		if err != nil {
			log.Println(err)
		} else {
			r.q.Add(resp.Roles...)
		}
	}
}

//func assumeRole(ctx context.Context, cfg *aws.Config, role string) {
//	svc := sts.NewFromConfig(*cfg)
//	l.Debug.Printf("Attempting to assume role %s\n", role)
//	// TODO: If this succeeds it ends up calling sts:AssumeRole twice as much as necessary.
//	_, err := svc.AssumeRole(ctx, &sts.AssumeRoleInput{
//		RoleArn:         aws.String(role.(string)),
//		RoleSessionName: aws.String("liquidswards-assumerole-test"),
//		DurationSeconds: aws.Int32(900),
//		ExternalId:      nil, // TODO: pass external ID along with role ARN
//	})
//
//	if err != nil {
//		l.Debug.Println(err.Error())
//	} else {
//		l.Info.Printf("Assumed role %s\n", role)
//
//		newCfg := *cfg
//		newCfg.Credentials = stscreds.NewAssumeRoleProvider(sts.NewFromConfig(*cfg), role)
//		go listRoles(nil)
//	}
//}

//func ListRoles(ctx context.Context, allCfgs *ListQueue) *ListQueue {
//	allRoles := NewListQueue()
//	go func() {
//		for cfg := range allCfgs.Each() {
//			err := listRoles(ctx, cfg.(*aws.Config), allRoles)
//			if err != nil {
//				l.Debug.Printf("Was not able to list roles with %s\n", *cfg.(*aws.Config))
//			}
//		}
//	}()
//	return allRoles
//}
//
//func listRoles(ctx context.Context, cfg *aws.Config, allRoles *ListQueue) error {
//	svc := iam.NewFromConfig(*cfg)
//
//	resp := &iam.ListRolesOutput{IsTruncated: true}
//
//	for resp.IsTruncated {
//		var err error
//		resp, err = svc.ListRoles(ctx, &iam.ListRolesInput{})
//		if err != nil {
//			return err
//		}
//
//		for _, r := range resp.Roles {
//			l.Debug.Printf("Found role %s\n", *r.Arn)
//			allRoles.Add(*r.Arn)
//		}
//	}
//	return nil
//}
//
//func AssumeRolesFromConfigs(ctx context.Context, allCfgs *ListQueue, roleCh *ListQueue) *sync.WaitGroup {
//	wg := &sync.WaitGroup{}
//	wg.Add(1)
//	go func() {
//		for cfg := range allCfgs.Each() {
//			go EnumRole(ctx, cfg.(*aws.Config), roleCh, allCfgs)
//		}
//		wg.Done()
//	}()
//	return wg
//}
//
//func EnumRole(ctx context.Context, cfg *aws.Config, roleCh *ListQueue, allCfgs *ListQueue) {
//	svc := sts.NewFromConfig(*cfg)
//	for role := range roleCh.Each() {
//		l.Debug.Printf("Attempting to assume role %s\n", role)
//		// TODO: If this succeeds it ends up calling sts:AssumeRole twice as much as necessary.
//		_, err := svc.AssumeRole(ctx, &sts.AssumeRoleInput{
//			RoleArn:         aws.String(role.(string)),
//			RoleSessionName: aws.String("liquidswards-assumerole-test"),
//			DurationSeconds: aws.Int32(900),
//			ExternalId:      nil, // TODO: pass external ID along with role ARN
//		})
//
//		if err != nil {
//			l.Debug.Println(err.Error())
//		} else {
//			l.Info.Printf("Assumed role %s\n", role)
//
//			newCfg := *cfg
//			newCfg.Credentials = stscreds.NewAssumeRoleProvider(sts.NewFromConfig(*cfg), role.(string))
//			allCfgs.Add(&newCfg)
//		}
//	}
//}
