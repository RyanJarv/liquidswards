package plugins

import (
	"fmt"
	"github.com/RyanJarv/liquidswards/lib/creds"
	"github.com/RyanJarv/liquidswards/lib/utils"
	"github.com/alitto/pond"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/cloudtrail"
	cloudtrailTypes "github.com/aws/aws-sdk-go-v2/service/cloudtrail/types"
	"github.com/aws/aws-sdk-go-v2/service/iam/types"
	"regexp"
	"sync"
	"time"
)

const MaxWorkers = 3
const MaxCapacity = MaxWorkers * 1000

var roleArnRe = regexp.MustCompile(`arn:aws:iam::[0-9]{12}:(role|assumed-role)/[-a-zA-Z_0-9+=,.@_/]+`)

type NewCloudTrailInput struct {
	GlobalPluginArgs
}

func NewCloudTrailPlugin(in *NewCloudTrailInput) *CloudTrail {
	pool := pond.New(MaxWorkers, MaxCapacity, pond.Strategy(pond.Lazy()))
	return &CloudTrail{
		NewCloudTrailInput: in,
		Pool:               pool,
		m:                  &sync.RWMutex{},
		covered:            map[string]bool{},
	}
}

type CloudTrail struct {
	*NewCloudTrailInput
	Pool    *pond.WorkerPool
	m       *sync.RWMutex
	covered map[string]bool
}

func (a *CloudTrail) Run(ctx utils.Context, cfg *creds.Config) {
	if a.Debug {
		utils.MonitorPoolStats(ctx, "cloudtrail worker pool:", a.Pool)
	}

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
		a.Lq.Wg.Add(1)
		a.Pool.Submit(func() {
			utils.SetDebugLabels("plugins", "cloudtrail", "arn", cfg.Arn())
			a.searchCloudTrail(ctx, cfg, slice.Start, slice.End)
		})
	}
}

func (a *CloudTrail) searchCloudTrail(ctx utils.Context, cfg *creds.Config, start, end time.Time) {
	defer func() {
		a.Lq.Wg.Done()

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
				if a.Lq.AddUnique(arn, types.Role{Arn: aws.String(arn)}) {
					fmt.Printf("CloudTrail: Found role %s\n", arn)
				}
			}
		}
	}
}
