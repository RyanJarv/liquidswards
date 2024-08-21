package types

import (
	"github.com/RyanJarv/liquidswards/lib/creds"
	"github.com/RyanJarv/liquidswards/lib/graph"
	"github.com/RyanJarv/liquidswards/lib/utils"
	"github.com/aws/aws-sdk-go-v2/aws"
)

type Plugin interface {
	Run(ctx utils.Context)
	Name() string
	Enabled() (enabled bool, reason string)
}

type Waitable interface {
	Wait()
}

type GlobalPluginArgs struct {
	Region           string
	FoundRoles       *utils.Iterator[Role]
	Access           *utils.Iterator[*creds.Config]
	Graph            *graph.Graph[*creds.Config]
	Scope            []string
	PrimaryAwsConfig aws.Config
	ProgramDir       string
	AwsConfigs       []*creds.Config
}

type NewPluginFunc func(utils.Context, GlobalPluginArgs) Plugin
