package plugins

import (
	"fmt"
	"github.com/RyanJarv/liquidswards/lib/creds"
	"github.com/RyanJarv/liquidswards/lib/types"
	"github.com/RyanJarv/liquidswards/lib/utils"
	"github.com/alitto/pond"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/cloudtrail"
	cloudtrailTypes "github.com/aws/aws-sdk-go-v2/service/cloudtrail/types"
	"regexp"
	"sync"
	"time"
)

var cloudtrailEnabled = utils.PluginArgs.Bool("cloudtrail", false, `
Search CloudTrail logs for sts:AssumeRole events. This can be used to discover roles that are assumed by other users.
`)

const MaxWorkers = 3
const MaxCapacity = MaxWorkers * 1000

var roleArnRe = regexp.MustCompile(`arn:aws:iam::[0-9]{12}:(role|assumed-role)/[-a-zA-Z_0-9+=,.@_/]+`)

func NewCloudTrail(ctx utils.Context, args types.GlobalPluginArgs) types.Plugin {
	pool := pond.New(MaxWorkers, MaxCapacity, pond.Strategy(pond.Lazy()))
	return &CloudTrail{
		enabled:          cloudtrailEnabled,
		Context:          ctx,
		GlobalPluginArgs: args,
		Pool:             pool,
		m:                &sync.RWMutex{},
		covered:          map[string]bool{},
	}
}

type CloudTrail struct {
	utils.Context
	types.GlobalPluginArgs
	Pool    *pond.WorkerPool
	m       *sync.RWMutex
	covered map[string]bool
	enabled *bool
}

func (a *CloudTrail) Name() string {
	return "cloudtrail"
}

func (a *CloudTrail) Enabled() (enabled bool, reason string) {
	if *a.enabled {
		return true, "will search cloudtrail for additional in-scope roles"
	} else {
		return false, "pass the -cloudtrail flag to enable"
	}
}

func (a *CloudTrail) Run(ctx utils.Context) {
	for _, node := range a.Graph.Nodes() {
		a.run(ctx, node.Value())
	}
}

func (a *CloudTrail) run(ctx utils.Context, cfg *creds.Config) {
	utils.MonitorPoolStats(ctx, "cloudtrail worker pool:", a.Pool)

	a.m.RLock()
	v, ok := a.covered[cfg.Account()]
	a.m.RUnlock()

	if ok || v {
		return
	} else {
		a.m.Lock()
		a.covered[cfg.Account()] = true
		a.m.Unlock()
	}

	slices := utils.TimeSlices(24*time.Hour, 60)
	for _, slice := range slices {
		a.Pool.Submit(func() {
			utils.SetDebugLabels("plugins", "cloudtrail", "arn", cfg.Arn())
			a.searchCloudTrail(ctx, cfg, slice.Start, slice.End)
		})
	}
}

func (a *CloudTrail) searchCloudTrail(ctx utils.Context, cfg *creds.Config, start, end time.Time) {
	defer func() {
		if r := recover(); r != nil {
			// Set covered back to false since this attempt failed.
			a.m.Lock()
			a.covered[cfg.Account()] = false
			a.m.Unlock()
		}
	}()
	svc := cloudtrail.NewFromConfig(cfg.Config())

	paginator := cloudtrail.NewLookupEventsPaginator(svc, &cloudtrail.LookupEventsInput{
		StartTime: &start,
		EndTime:   &end,
		LookupAttributes: []cloudtrailTypes.LookupAttribute{
			{
				AttributeKey:   cloudtrailTypes.LookupAttributeKeyEventName,
				AttributeValue: aws.String("AssumeRole"),
			},
		},
	})

	for paginator.HasMorePages() && ctx.IsRunning("finished searching cloudtrail, exiting...") {
		fmt.Printf(".")
		page, err := paginator.NextPage(ctx)
		if err != nil {
			panic(err)
		}
		for _, event := range page.Events {
			all := roleArnRe.FindAll([]byte(*event.CloudTrailEvent), -1)
			for _, role := range all {
				arn := string(role)
				if a.Scope != nil && !utils.ArnInScope(a.Scope, arn) {
					continue
				}
				if a.FoundRoles.Add(types.NewRole(arn)) {
					fmt.Printf("CloudTrail: Found role %s\n", arn)
				}
			}
		}
	}
}
