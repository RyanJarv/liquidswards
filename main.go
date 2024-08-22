package main

import (
	"context"
	"flag"
	"fmt"
	"github.com/RyanJarv/liquidswards/lib/creds"
	"github.com/RyanJarv/liquidswards/lib/graph"
	"github.com/RyanJarv/liquidswards/lib/plugins"
	"github.com/RyanJarv/liquidswards/lib/types"
	"github.com/RyanJarv/liquidswards/lib/utils"
	"github.com/alitto/pond"
	"os"
	"path/filepath"
	"strings"
)

const MaxWorkers = 100
const MaxCapacity = 1000

var (
	ctx = utils.NewContext(context.Background())

	region   = flag.String("region", "us-east-1", "The AWS Region to use")
	scopeStr = flag.String("scope", "", `
List of AWS account ID's (seperated by comma's) that are in scope. Accounts associated with any profiles used are 
always in scope regardless of this value.
`)
	noScope = flag.Bool("no-scope", false, `
Disable scope, all discovered role ARN's belonging to ANY account will be enumerated for access and additional file 
recursively.
`)
	profilesStr = flag.String("profiles", "default", "List of AWS profiles (seperated by commas)")
	name        = flag.String("name", "default", "Name of environment, used to store and retrieve graphs.")
	noSave      = flag.Bool("no-save", false, "Do not save scan results to disk.")
	load        = flag.Bool("load", false, "Load results from previous scans.")
	debug       = flag.Bool("debug", false, "Enable debug output")

	help = strings.Replace(`
liquidswards discovers and enumerates access to IAM Roles via sts:SourceAssumeRole API call's. For each account \
associated with a profile passed on the command line it will discover roles via iam:ListRoles and searching CloudTrail \
(if the -cloudtrail argument is used) for sts:SourceAssumeRole calls by other users. For each discovered role \
sts:AssumeRole will be tested from all currently maintained access, if the call succeeds the discovery and access \
enumeration steps are repeated from that Role if necessary, and the role is added to the access pool. To summarize, \
it attempts to recursively discover and enumerate all possible sts:SourceAssumeRole paths that exist from the profiles \
passed on the command line.

We purposefully avoid relying on IAM parsing extensively due to the complexity involved as well as the goal of \
discovering what is known to be possible rather then what we think is possible.

The tool maintains a graph which is persisted to disk of the roles that where accessed. This is stored in \
~/.liquidswards/<name>/ where name is the argument passed by -name. This can be used to save and load different \
sessions.

`, "\\\n", "", -1)

	allPlugins = []types.NewPluginFunc{
		plugins.NewCloudTrail,
		plugins.NewSqs,
		plugins.NewFile,
		plugins.NewRefresh,
		plugins.NewAssume,
		plugins.NewList,
	}
)

func main() {
	flag.Usage = func() {
		w := flag.CommandLine.Output()
		utils.Must(fmt.Fprintf(w, "Main arguments:\n"))
		flag.PrintDefaults()
		utils.Must(fmt.Fprintf(w, "About liquidswards:\n"))
		utils.Must(fmt.Fprintf(w, help))
	}

	flag.Parse()

	if *debug {
		ctx.SetLoggingLevel(utils.DebugLogLevel)
	}

	if *region != "" {
		ctx.Debug.Printf("using region %s\n", *region)
	}

	if len(flag.Args()) > 1 {
		ctx.Error.Fatalln("extra arguments detected, did you mean to pass a comma seperated list to -profiles instead?")
	}

	if err := Run(); err != nil {
		ctx.Error.Fatalln(err)
	}

}

func Run() error {
	graph := graph.NewDirectedGraph[*creds.Config]()

	programDir := utils.Must(GetProgramDir(*name))
	graphPath := filepath.Join(programDir, "nodes.json")

	if *load {
		if err := graph.Load(graphPath); err != nil {
			return fmt.Errorf("error loading graph: %w", err)
		}
	}

	if len(flag.Args()) == 1 {
		if err := graph.Load(graphPath); err != nil {
			return fmt.Errorf("error loading graph: %w", err)
		}
		return PrintCreds(graph, flag.Args()[0])
	}

	// TODO: Move to assumeroles?
	cfgs, err := creds.ParseProfiles(ctx, *profilesStr, *region, graph)
	if err != nil {
		ctx.Error.Fatalf("parsing profiles: %s\n", err)
	}

	var scope []string
	if !*noScope {
		scope = creds.ParseScope(*scopeStr, cfgs)
		ctx.Info.Printf("scope is currently set to: %s\n", strings.Join(scope, ", "))
	} else {
		ctx.Info.Printf("scope is not currently set!!!")
	}

	scanCtx := ScanContext(ctx)

	pool := pond.New(MaxWorkers, MaxCapacity, pond.Strategy(pond.Eager()))
	if *debug {
		utils.MonitorPoolStats(scanCtx, "assumeRole worker pool:", pool)
	}

	args := types.GlobalPluginArgs{
		Region:           *region,
		FoundRoles:       utils.NewIterator[types.Role](),
		Access:           utils.NewIterator[*creds.Config](),
		Graph:            graph,
		Scope:            scope,
		ProgramDir:       programDir,
		PrimaryAwsConfig: cfgs[0].Config,
		AwsConfigs:       cfgs,
	}

	var waitable []types.Waitable
	// Plugins typically get run when a role is discovered, if the scan is skipped we need to trigger them here.
	for _, plugin := range allPlugins {
		p := plugin(scanCtx, args)

		if enabled, reason := p.Enabled(); enabled {
			ctx.Info.Printf("plugin %s is enabled: %s\n", p.Name(), reason)
			p.Run(ctx)

			if w, ok := p.(types.Waitable); ok {
				waitable = append(waitable, w)
			}
		} else {
			ctx.Info.Printf("plugin %s is disabled: %s\n", p.Name(), reason)
		}
	}

	for _, cfg := range cfgs {
		args.FoundRoles.Add(types.NewRole(cfg.Arn()))
		args.Access.Add(cfg)
	}

	for _, w := range waitable {
		w.Wait()
	}

	if !*noSave {
		err := graph.Save(graphPath)
		if err != nil {
			ctx.Error.Fatalf("error saving report: %s\n", err)
		}
	}

	if len(graph.Nodes()) != 0 {
		graphVizPath := filepath.Join(args.ProgramDir, "graph.dot")
		err = graph.Report(ctx, cfgs, graphVizPath)
		if err != nil {
			ctx.Error.Fatalf("generating report failed: %s\n", err)
		}

		fmt.Printf("\n\tGraphviz saved to %s. To convert this to an image use one of the following commands:\n", graphVizPath)
		fmt.Printf("\t\tdot -Tpng %s -o graph.png\n", graphVizPath)
		fmt.Printf("\t\tcirco -Tpng %s -o graph.png\n", graphVizPath)
		fmt.Println("\n\tOr if the graph is to complex you can simplify it by removing redundant paths first:")
		fmt.Printf("\t\ttred %s | dot -Tpng /dev/stdin -o graph.png\n", graphVizPath)
		fmt.Printf("\t\ttred %s | circo -Tpng /dev/stdin -o graph.png\n", graphVizPath)
	}

	return nil
}

func PrintCreds(g *graph.Graph[*creds.Config], arn string) error {
	node, ok := g.GetNode(arn)
	if !ok {
		return fmt.Errorf("role %s not found in graph", arn)
	}

	cfg := node.Value()
	creds, err := cfg.Credentials.Retrieve(ctx)
	if err != nil {
		return fmt.Errorf("error retrieving credentials: %w", err)
	}

	fmt.Printf("AWS_ACCESS_KEY_ID=%s AWS_SECRET_ACCESS_KEY=%s AWS_SESSION_TOKEN=%s",
		creds.AccessKeyID, creds.SecretAccessKey, creds.SessionToken)

	return nil
}

func GetProgramDir(name string) (string, error) {
	path, err := utils.ExpandPath(fmt.Sprintf("~/.liquidswards/%s", name))
	if err != nil {
		ctx.Error.Fatalln("failed to expand path: %w", err)
	}

	err = os.MkdirAll(path, os.FileMode(0750))
	if err != nil {
		ctx.Error.Fatalf("unable to create directory %s: %s\n", path, err)
	}
	return path, err
}

func ScanContext(ctx utils.Context) utils.Context {
	sigs := utils.SigTermChan()
	scanCtx, cancelScan := ctx.WithCancel()
	go func() {
		<-sigs
		ctx.Error.Println("Received signal, cancelling scan...")
		cancelScan()
	}()
	return scanCtx
}
