package plugins

import (
	"github.com/RyanJarv/liquidswards/lib/creds"
	"github.com/RyanJarv/liquidswards/lib/graph"
	"github.com/RyanJarv/liquidswards/lib/utils"
	"github.com/RyanJarv/lq"
	"github.com/aws/aws-sdk-go-v2/service/iam/types"
)

type DiscoveryPlugin interface {
	Run(ctx utils.Context, cfg *creds.Config)
}

type GlobalPluginArgs struct {
	Debug  bool
	Region string
	Lq     *lq.ListQueue[types.Role]
	Graph  *graph.Graph[*creds.Config]
	Scope  []string
}
