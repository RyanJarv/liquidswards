package main

import (
	"context"
	"flag"
	"fmt"
	"github.com/RyanJarv/liquidswards/lib"
	"github.com/RyanJarv/liquidswards/lib/creds"
	"github.com/RyanJarv/liquidswards/lib/utils"
	"github.com/RyanJarv/lq"
	"github.com/alitto/pond"
	"github.com/aws/aws-sdk-go-v2/service/iam/types"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

const MaxWorkers = 100
const MaxCapacity = 1000

var (
	region      string
	scopeStr    string
	profilesStr string
	name        string
	noScan      bool
	noSave      bool
	load        bool
	debug       bool
	ctx         = utils.NewContext(context.Background())
)

func main() {
	flag.Usage = func() {
		w := flag.CommandLine.Output() // may be os.Stderr - but not necessarily
		fmt.Fprintln(w, "Usage of liquidswards:")

		flag.PrintDefaults()

		fmt.Fprintln(
			w,
			`
	liquidswards discovers and enumerates access to IAM Roles via
sts:AssumeRole API call's. For each account associated with a profile
passed on the command line it will discover roles via iam:ListRoles. For each
role accessed it will attempt to call sts:AssumeRole targeting every other role
discovered so far, if the call succeeds the discovery and access enumeration steps
are repeated from that Role if needed. In other words, it attempts to recursively
discover and enumerate all possible sts:AssumeRole paths that exist in the account
profiles passed on the command line.

	We purposefully avoid relying on IAM parsing for discovering relationships. This
is due to the complexity of IAM as well as the goal of discovering what is actually
possible rather then what we think is possible (TODO make this configurable to reduce
API calls).

	liquidswards maintains a graph which is persisted to disk of roles that where
accessed. This is stored in ~/.liquidswards/<name>/ based on the -name argument. This
can be used to save and load unique sessions. The graph is used internally to build a
GraphViz .dot file at the end of the run which can be converted to an image of accessible
file. A simplified version of this graph with some info removed is also outputted to the
console as well.`)
	}

	flag.StringVar(&region, "region", "us-east-1", "The AWS Region to use")
	flag.StringVar(&scopeStr, "scope", "",
		"List of AWS account ID's (seperated by comma's) that are in scope. \n"+
			"Accounts associated with any profiles used are always in scope \n"+
			"regardless of this value.")
	flag.StringVar(&profilesStr, "profiles", "", "List of AWS profiles (seperated by commas)")
	flag.BoolVar(&noScan, "no-scan", false, "Do not attempt to assume file any file.")
	flag.BoolVar(&noSave, "no-save", false, "Do not save scan results to disk.")
	flag.BoolVar(&load, "load", false, "Load results from previous scans.")
	flag.StringVar(&name, "name", "default", "Name of environment, used to store and retrieve graphs.")
	flag.BoolVar(&debug, "debug", false, "Enable debug output")
	flag.Parse()

	if debug {
		ctx.SetLoggingLevel(utils.DebugLogLevel)
	}

	if region != "" {
		ctx.Debug.Printf("using region %s\n", region)
	}

	if len(flag.Args()) > 1 {
		ctx.Error.Fatalln("extra arguments detected, did you mean to pass a comma seperated list to -profiles instead?")
	}

	path, err := utils.ExpandPath(fmt.Sprintf("~/.liquidswards/%s", name))
	if err != nil {
		ctx.Error.Fatalln("failed to expand path: %w", err)
	}

	err = os.MkdirAll(path, os.FileMode(0750))
	if err != nil {
		ctx.Error.Fatalf("unable to create directory %s: %s\n", path, err)
	}

	graph := lib.NewGraph()
	// TODO: Move to assumeroles? (think this comment got accidentally updated during some refactor)
	cfgs, err := creds.ParseProfiles(ctx, profilesStr, region, graph)
	if err != nil {
		ctx.Error.Fatalf("parsing profiles: %s\n", err)
	}

	var scope []string
	scope = creds.ParseScope(scopeStr, cfgs)
	ctx.Info.Printf("scope is currently set to: %s\n", strings.Join(scope, ", "))

	scanCtx := ScanContext(ctx)

	pool := pond.New(MaxWorkers, MaxCapacity, pond.Strategy(pond.Eager()))
	if debug {
		utils.MonitorPoolStats(scanCtx, "assumeRole worker pool:", pool)
	}

	assume := lib.AssumeAllRoles(pool, graph, lq.NewListQueue[types.Role](), scope, region)

	graphPath := filepath.Join(path, "nodes.json")
	if load {
		err = assume.Graph.Load(graphPath)
		for _, node := range assume.Graph.Nodes() {
			node.Value().SetGraph(assume.Graph)
		}
		if err != nil {
			ctx.Info.Printf("error loading graph, skipping: %s\n", err)
		}
	}

	if !noScan {
		err = assume.Scan(scanCtx, cfgs)
		if err != nil {
			ctx.Error.Fatalf("scan failed: %s\n", err)
		}
	}

	<-scanCtx.Done()

	//a.Lq.Wait()

	// TODO: fix this
	// the lq wait group seems to be slightly off so wait a sec after the scan as a hack
	time.Sleep(time.Second)

	if !noSave {
		err := assume.Graph.Save(graphPath)
		if err != nil {
			ctx.Error.Fatalf("error saving report: %s\n", err)
		}
	}

	if len(assume.Graph.Nodes()) != 0 {
		graphVizPath := filepath.Join(path, "graph.dot")
		err = assume.Report(ctx, cfgs, graphVizPath)
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

}

func ScanContext(ctx utils.Context) utils.Context {
	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)

	scanCtx, cancelScan := ctx.WithCancel()
	go func() {
		<-sigs
		ctx.Error.Println("Received signal, cancelling scan...")
		cancelScan()
	}()
	return scanCtx
}
