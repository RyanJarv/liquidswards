package plugins

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"github.com/RyanJarv/liquidswards/lib/creds"
	"github.com/RyanJarv/liquidswards/lib/graph"
	"github.com/RyanJarv/liquidswards/lib/types"
	"github.com/RyanJarv/liquidswards/lib/utils"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/sqs"
	sqsTypes "github.com/aws/aws-sdk-go-v2/service/sqs/types"
)

var sqsQueue = flag.String("sqs-queue", "", `
SQS queue which receives IAM updates via CloudTrail/CloudWatch/EventBridge. If set, -access-CredRefreshSeconds is not used and 
access is only refreshed when the credentials are about to expire or access is revoked via the web console. 

Currently, the first profile passed with -profiles is used to access the SQS queue. 

TODO: Make the profile used to access the queue configurable.
`)

type NewSqsInput struct {
	types.GlobalPluginArgs
}

func NewSqs(ctx utils.Context, args types.GlobalPluginArgs) types.Plugin {
	return &Sqs{
		GlobalPluginArgs: args,
		SqsQueue:         *sqsQueue,
		cfgs:             map[string][]chan int{},
	}
}

type Sqs struct {
	types.GlobalPluginArgs

	// SqsQueue is an SQS queue that we assume is configured to receive IAM updates. This allows us to CredRefreshSeconds
	// credentials only when necessary. AccessRefresh is ignored when this is specified.
	SqsQueue string
	Path     string
	cfgs     map[string][]chan int
}

func (a *Sqs) Name() string {
	return "sqs"
}

func (a *Sqs) Enabled() (bool, string) {
	if a.SqsQueue != "" {
		return true, fmt.Sprintf("will CredRefreshSeconds on revocation events from %s", a.SqsQueue)
	} else {
		return false, "no -sqs-queue arg provided"
	}
}

func (a *Sqs) Run(ctx utils.Context) {
	svc := sqs.NewFromConfig(a.PrimaryAwsConfig)
	go a.RunSqsClient(ctx, svc.ReceiveMessage, svc.DeleteMessage, a.Graph.GetNode)
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
			UserName       *string `json:"userName"`
			RoleName       *string `json:"roleName"`
			PolicyName     *string `json:"policyName"`
			PolicyDocument *string `json:"policyDocument"`
		} `json:"requestParameters"`
		RecipientAccountId string `json:"recipientAccountId"`
	} `json:"detail"`
}

type ReceiveMessageFunc func(ctx context.Context, params *sqs.ReceiveMessageInput, optFns ...func(*sqs.Options)) (*sqs.ReceiveMessageOutput, error)
type DeleteMessageFunc func(ctx context.Context, params *sqs.DeleteMessageInput, optFns ...func(*sqs.Options)) (*sqs.DeleteMessageOutput, error)
type GetNode func(k string) (graph.Node[*creds.Config], bool)

func (a *Sqs) RunSqsClient(ctx utils.Context, receive ReceiveMessageFunc, delete DeleteMessageFunc, getNode GetNode) {
	for ctx.IsRunning() {
		msg, err := receive(ctx, &sqs.ReceiveMessageInput{
			QueueUrl:            aws.String(a.SqsQueue),
			MaxNumberOfMessages: 1,
			VisibilityTimeout:   5,
			WaitTimeSeconds:     20,
		})
		if err != nil {
			ctx.Error.Printf("failed receiving message from %s: %s\n", a.SqsQueue, err)
			continue
		}

		for _, msg := range msg.Messages {
			if _, err := delete(ctx, &sqs.DeleteMessageInput{
				QueueUrl:      aws.String(a.SqsQueue),
				ReceiptHandle: msg.ReceiptHandle,
			}); err != nil {
				ctx.Error.Printf("failed deleting message from %s: %s\n", a.SqsQueue, err)
			}

			if err := handleCloudTrailMsg(ctx, getNode, msg); err != nil {
				fmt.Printf("failed to handle cloudtrail message: %s\n", err)
			}
		}
	}
}

func handleCloudTrailMsg(ctx utils.Context, get GetNode, msg sqsTypes.Message) error {
	event := CloudTrailEvent{}
	if err := json.Unmarshal([]byte(*msg.Body), &event); err != nil {
		return fmt.Errorf("failed to unmarshal event: %s\n", err)
	}

	detail := event.Detail
	params := event.Detail.RequestParameters
	if detail.EventName == "PutRolePolicy" && params.PolicyName != nil && *params.PolicyName == "AWSRevokeOlderSessions" {
		arn := fmt.Sprintf("arn:aws:iam::%s:role/%s", detail.RecipientAccountId, *detail.RequestParameters.RoleName)
		node, ok := get(arn)
		if !ok {
			return fmt.Errorf("sqs: no config found for %s", arn)
		}

		if creds, err := node.Value().Refresh(ctx); err != nil {
			return fmt.Errorf("sqs: failed to CredRefreshSeconds %s: %s", arn, err)
		} else {
			ctx.Info.Printf("refreshed %s -- %s", arn, creds.AccessKeyID)
		}
	}
	return nil
}
