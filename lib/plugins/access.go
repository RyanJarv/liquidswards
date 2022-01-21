package plugins

import (
	"context"
	"encoding/json"
	"github.com/RyanJarv/liquidswards/lib/creds"
	"github.com/RyanJarv/liquidswards/lib/utils"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/sqs"
	"strings"
	"time"
)

type NewAccessInput struct {
	GlobalPluginArgs
	Context utils.Context
	Path    string

	// By the time session revocation through the UI is processed it appears to limit sessions for about 6 seconds
	// in the past. This gives us a 6-second window where new sessions are not affected in any way.
	AccessRefresh int

	// SnsQueue is an SNS queue that we assume is configured to receive IAM updates. This allows us to refresh
	// credentials only when necessary. AccessRefresh is ignored when this is specified.
	SnsQueue  string
	SnsConfig aws.Config
}

func NewAccess(in *NewAccessInput) (*Access, error) {
	access := &Access{
		NewAccessInput: in,
		cfgs:           map[string][]chan int{},
	}
	if in.SnsQueue != "" {
		svc := sqs.NewFromConfig(in.SnsConfig)
		go access.RunSqsClient(in.Context, svc.ReceiveMessage, svc.DeleteMessage)
	}
	return access, nil
}

type Access struct {
	*NewAccessInput
	cfgs map[string][]chan int
}

type CloudTrailEvent struct {
	Version    string `json:"version"`
	Id         string `json:"id"`
	DetailType string `json:"detail-type"`
	Source     string `json:"source"`
	Account    string `json:"account"`
	Time       string `json:"time"`
	Detail     struct {
		EventName         string `json:"eventName"`
		RequestParameters struct {
			RoleName       string `json:"roleName"`
			PolicyName     string `json:"policyName"`
			PolicyDocument string `json:"policyDocument"`
		} `json:"requestParameters"`
		RecipientAccountId string `json:"recipientAccountId"`
	} `json:"detail"`
}

type RecieveMessageFunc func(ctx context.Context, params *sqs.ReceiveMessageInput, optFns ...func(*sqs.Options)) (*sqs.ReceiveMessageOutput, error)
type DeleteMessageFunc func(ctx context.Context, params *sqs.DeleteMessageInput, optFns ...func(*sqs.Options)) (*sqs.DeleteMessageOutput, error)

func (a *Access) RunSqsClient(ctx utils.Context, recieve RecieveMessageFunc, delete DeleteMessageFunc) {
	for ctx.IsRunning() {
		msg, err := recieve(ctx, &sqs.ReceiveMessageInput{
			QueueUrl:            aws.String(a.SnsQueue),
			MaxNumberOfMessages: 1,
			VisibilityTimeout:   5,
			WaitTimeSeconds:     20,
		})
		if err != nil {
			ctx.Error.Printf("failed receiving message from %s: %s\n", a.SnsQueue, err)
			continue
		}

		for _, msg := range msg.Messages {
			delete(ctx, &sqs.DeleteMessageInput{
				QueueUrl:      aws.String(a.SnsQueue),
				ReceiptHandle: msg.ReceiptHandle,
			})

			var event CloudTrailEvent
			err := json.Unmarshal([]byte(*msg.Body), &event)
			if err != nil {
				ctx.Error.Printf("failed to unmarshal event: %s\n", err)
				continue
			}

			params := event.Detail.RequestParameters
			if params.PolicyName == "AWSRevokeOlderSessions" {
				// TODO: Only refresh the specific role that was revoked.
				// Need to make sure this works when the role has a path, for now we just refresh all roles with a
				// matching name.
				ctx.Debug.Println("received revocation event from sqs queue for", params.RoleName)
				for _, refreshCh := range a.cfgs[params.RoleName] {
					select {
					case refreshCh <- 0:
					default:
						ctx.Info.Printf("refresh already in progress for %s, dropping event", params.RoleName)
					}
				}
			}
		}
	}
}

func (a *Access) Run(ctx utils.Context, cfg *creds.Config) {
	c, err := cfg.Config().Credentials.Retrieve(ctx)
	if err != nil {
		ctx.Error.Println("access plugin: run: failed to determine source")
	}

	// All roles we maintain should have the source set to ARN of the principal used to retrieve the current creds.
	if !strings.HasPrefix(c.Source, "arn:aws:") {
		ctx.Debug.Printf("skipping role juggling of %s\n", cfg.Arn())
		return
	}

	a.Lq.Wg.Add(1)
	go func() {
		utils.SetDebugLabels("plugins", "access", "identity", cfg.Arn())

		defer func() {
			a.Lq.Wg.Done()
			if r := recover(); r != nil {
				ctx.Error.Println("access:", r)
			}
		}()

		a.cfgs[cfg.Name()] = append(a.cfgs[cfg.Name()], a.cycleAccess(ctx, cfg))
	}()
}

// cycleAccess refreshes credentials periodically and when triggered by the returned channel.
//
// This allows us to catch and refresh revoked sessions, the time between refreshes is configured with -access-refresh.
func (a *Access) cycleAccess(ctx utils.Context, cfg *creds.Config) chan int {
	refreshCh := make(chan int, 10)
	go func() {
		for {
			select {
			case <-time.After(time.Second * time.Duration(a.NewAccessInput.AccessRefresh)):
			case <-refreshCh:
			}
			ctx.Info.Println("refreshing:", cfg.Arn())

			_, err := cfg.Refresh(ctx)
			if err != nil {
				ctx.Error.Printf("periodic refresh failed: %s\n", err)
				continue
			}
			ctx.Info.Println("refreshed:", cfg.Arn())
		}
	}()
	return refreshCh
}
