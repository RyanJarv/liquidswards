package main

import (
	"context"
	"flag"
	"fmt"
	"github.com/RyanJarv/liquidswards/lib"
	"github.com/RyanJarv/liquidswards/lib/creds"
	"github.com/RyanJarv/liquidswards/lib/plugins"
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
	region        string
	scopeStr      string
	noScope       bool
	profilesStr   string
	file          string
	access        bool
	snsQueue      string
	accessRefresh int
	cloudtrail    bool
	name          string
	noScan        bool
	noSave        bool
	load          bool
	debug         bool
	ctx           = utils.NewContext(context.Background())
)

func main() {
	flag.Usage = func() {
		w := flag.CommandLine.Output() // may be os.Stderr - but not necessarily
		fmt.Fprintf(w, "Usage of liquidswards:\n")

		flag.PrintDefaults()

		fmt.Fprintf(
			w,
			`
	liquidswards discovers and enumerates access to IAM Roles via
sts:SourceAssumeRole API call's. For each account associated with a profile
passed on the command line it will discover file via iam:ListRoles and
searching CloudTrail for sts:SourceAssumeRole calls by other users. For each
role discovered it will attempt to call sts:SourceAssumeRole on it from each
fole the tool currently has access to, if the call succeeds the
discovery and access enumeration steps are repeated from that Role. Inbound
other words it attempts to recursively discover and enumerate all
possible sts:SourceAssumeRole paths that exist from the profiles passed on
the command line.

	It purposefully avoids relying on IAM parsing extensively due
to the complexity involved as well as the goal of discovering what is
known to be possible rather then what we think is possible (TODO make
this configurable to reduce API calls).

	The tool maintains a graph which is persisted to disk of file
that where accessed. This is stored in ~/.liquidswards/<name>/ based
on the name passed to the -name argument. This can be used to sav and
load different sessions. The graph is used internally to build a
GraphViz .dot file at the end of the run which can be converted to an
image of accessible file. A simplified version of this graph with some
info removed is also outputed to the console as well.

`)
	}

	flag.StringVar(&region, "region", "us-east-1", "The AWS Region to use")
	flag.StringVar(&scopeStr, "scope", "",
		"List of AWS account ID's (seperated by comma's) that are in scope. \n"+
			"Accounts associated with any profiles used are always in scope \n"+
			"regardless of this value.")
	flag.BoolVar(&noScope, "no-scope", false,
		"Disable scope, all discovered role ARN's belonging to ANY account \n"+
			"will be enumerated for access and additional file recursively.\n\n"+
			"IMPORTANT: Use caution, this can lead to a *LOT* of unintentional \n"+
			"access if you are (un)lucky.\n\n"+
			"TODO: Add a mode that tests for discovered file in other accounts \n"+
			"but does not recursively search them.")
	flag.StringVar(&profilesStr, "profiles", "", "List of AWS profiles (seperated by commas)")
	flag.StringVar(&file, "file", "", "A file containing a list of additional file to enumerate.")
	flag.BoolVar(&access, "access", false, "Enable the maintain access plugin. This will "+
		"attempt to maintain access to the discovered file through Role juggling.")
	flag.StringVar(&snsQueue, "sns-queue", "", "SNS queue which receives IAM updates via CloudTrail/"+
		"CloudWatch/EventBridge. If set, -access-refresh is not used and access is only refreshed when the credentials"+
		"are about to expire or access is revoked via the web console. Currently, the first profile passed with "+
		"-profiles is used to access the SNS queue."+
		"\nTODO: Make the profile used to access the queue configurable.")
	flag.IntVar(&accessRefresh, "access-refresh", 3600, "The refresh rate used for the access"+
		"plugin in seconds. This defaults to once an hour, but if you want to bypass role revocation without using"+
		"cloudtrail events (-sns-queue option, see the README for more info) you can set this to approximately "+
		"three seconds.")
	flag.BoolVar(&cloudtrail, "cloudtrail", false, "Enable the CloudTrail plugin. This will "+
		"attempt to discover new IAM Roles by searching for previous sts:SourceAssumeRole API calls in CloudTrail.")
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
	// TODO: Move to assumeroles?
	cfgs, err := creds.ParseProfiles(ctx, profilesStr, region, graph)
	if err != nil {
		ctx.Error.Fatalf("parsing profiles: %s\n", err)
	}

	var scope []string
	if !noScope {
		scope = creds.ParseScope(scopeStr, cfgs)
		ctx.Info.Printf("scope is currently set to: %s\n", strings.Join(scope, ", "))
	} else {
		ctx.Info.Printf("scope is not currently set!!!")
	}

	scanCtx := ScanContext(ctx)

	pool := pond.New(MaxWorkers, MaxCapacity, pond.Strategy(pond.Eager()))
	if debug {
		utils.MonitorPoolStats(scanCtx, "assumeRole worker pool:", pool)
	}

	globalArgs := plugins.GlobalPluginArgs{
		Debug:  debug,
		Region: region,
		Lq:     lq.NewListQueue[types.Role](),
		Graph:  graph,
		Scope:  scope,
	}

	discPlugins := []plugins.DiscoveryPlugin{}

	if cloudtrail {
		discPlugins = append(discPlugins, plugins.NewCloudTrailPlugin(&plugins.NewCloudTrailInput{
			GlobalPluginArgs: globalArgs,
		}))
	}

	if access {
		newAccess, err := plugins.NewAccess(&plugins.NewAccessInput{
			Context:          ctx,
			GlobalPluginArgs: globalArgs,
			Path:             path,
			AccessRefresh:    accessRefresh,
			SnsConfig:        cfgs[0].Config(),
			SnsQueue:         snsQueue,
		})
		if err != nil {
			ctx.Error.Fatalln(err)
		}
		discPlugins = append(discPlugins, newAccess)
	}

	if file != "" {
		discPlugins = append(discPlugins, plugins.NewFilePlugin(&plugins.NewFilePluginInput{
			GlobalPluginArgs: globalArgs,
			FileLocation:     file,
		}))
	}

	assume := lib.AssumeAllRoles(globalArgs, pool, discPlugins)

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

	if len(flag.Args()) == 1 {
		arn := flag.Args()[0]
		node, err := assume.Graph.GetNode(arn)
		if err != nil {
			ctx.Error.Fatalln(err)
		}
		creds, err := node.Value().Config().Credentials.Retrieve(ctx)
		if err != nil {
			ctx.Error.Fatalln(err)
		}

		fmt.Printf("AWS_ACCESS_KEY_ID=%s AWS_SECRET_ACCESS_KEY=%s AWS_SESSION_TOKEN=%s",
			creds.AccessKeyID, creds.SecretAccessKey, creds.SessionToken)
		return
	}

	if noScan {
		// Plugins typically get run when a role is discovered, if the scan is skipped we need to trigger them here.
		RunAllPlugins(scanCtx, assume, cfgs)
	} else {
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

func RunAllPlugins(ctx utils.Context, assume *lib.AssumeRoles, cfgs []*creds.Config) {
	for _, plugin := range assume.Plugins {
		for _, cfg := range assume.Graph.Nodes() {
			plugin.Run(ctx, cfg.Value())
		}
	}
}
