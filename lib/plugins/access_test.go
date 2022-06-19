package plugins

import (
	"context"
	"github.com/RyanJarv/liquidswards/lib/creds"
	"github.com/RyanJarv/liquidswards/lib/graph"
	"github.com/RyanJarv/liquidswards/lib/utils"
	"github.com/RyanJarv/lq"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/iam/types"
	"github.com/aws/aws-sdk-go-v2/service/sqs"
	types2 "github.com/aws/aws-sdk-go-v2/service/sqs/types"
	"github.com/aws/smithy-go/middleware"
	"testing"
)

const (
	testsqsQueue  = "https://127.0.0.1"
	testAccountId = "123456789012"
	testRegion    = "us-east-1"
	testPath      = "/test/path"
)

var ctx = utils.NewContext(context.Background())

func TestNewAccess(t *testing.T) {
	g := graph.NewDirectedGraph[*creds.Config]()
	cfg := utils.Must(creds.NewTestAssumesAllConfig(creds.SourceProfile, "user/profile-a", nil))
	g.AddNode(cfg)
	_, err := NewTestAccess(g)
	if err != nil {
		t.Fatal(err)
	}
}

func TestAccess_Run(t *testing.T) {
	g := graph.NewDirectedGraph[*creds.Config]()
	cfg := utils.Must(creds.NewTestAssumesAllConfig(creds.SourceProfile, "user/profile-a", g))
	g.AddNode(cfg)
	access, err := NewTestAccess(g)
	if err != nil {
		t.Fatal(err)
	}

	access.Run(ctx, cfg)

	want := "active"
	if got := cfg.State(); got != creds.ActiveState {
		t.Errorf("got %q, want %q", got, want)
	}
	cfg.SetState(creds.RefreshingState)
}

func TestAccess_RunSqsClient(t *testing.T) {
	access := &Access{
		NewAccessInput: &NewAccessInput{
			Context:          ctx,
			GlobalPluginArgs: GlobalPluginArgs{Debug: true, Region: "us-east-1"},
			SqsQueue:         "https://test-sqs-queue.local",
			SqsConfig:        aws.Config{Region: "us-east-1"},
		},
		cfgs: map[string][]chan int{
			"role-a": {make(chan int, 10), make(chan int, 10)},
			"role-b": {make(chan int, 10)},
			"role-c": {},
		},
	}

	nameCh := make(chan string)
	sqsCtx, cancel := ctx.WithCancel()

	go access.RunSqsClient(sqsCtx, func(ctx context.Context, params *sqs.ReceiveMessageInput, optFns ...func(*sqs.Options)) (*sqs.ReceiveMessageOutput, error) {
		return &sqs.ReceiveMessageOutput{
			Messages: []types2.Message{
				{Body: revokeSessMsg(<-nameCh)},
			},
			ResultMetadata: middleware.Metadata{},
		}, nil
	}, func(ctx context.Context, params *sqs.DeleteMessageInput, optFns ...func(*sqs.Options)) (*sqs.DeleteMessageOutput, error) {
		return &sqs.DeleteMessageOutput{}, nil
	})

	nameCh <- "role-a"
	nameCh <- "role-b"
	nameCh <- "role-c"
	cancel()

	for name, refreshChs := range access.cfgs {
		for i, refreshCh := range refreshChs {
			select {
			case <-refreshCh:
				ctx.Info.Printf("refreshed chan # %d of %s\n", i, name)
			default:
				t.Errorf("no refresh recieved for chan # %d of %s", i, name)
			}
		}
	}
}

func revokeSessMsg(name string) *string {
	return aws.String(`{
	  "version": "0",
	  "id": "b0f351e0-8ee9-dc51-0f64-682db2c0e8fd",
	  "detail-type": "AWS API Call via CloudTrail",
	  "source": "aws.iam",
	  "account": "336983520827",
	  "time": "2022-02-16T01:36:00Z",
	  "detail": {
		"eventName": "PutRolePolicy",
		"requestParameters": {
		  "roleName": "` + name + `",
		  "policyName": "AWSRevokeOlderSessions",
		  "policyDocument": "{\"Version\":\"2012-10-17\",\"Statement\":[{\"Effect\":\"Deny\",\"Action\":[\"*\"],\"Resource\":[\"*\"],\"Condition\":{\"DateLessThan\":{\"aws:TokenIssueTime\":\"2022-02-16T01:36:00.138Z\"}}}]}"
		},
		"recipientAccountId": "336983520827"
	  }
	}`)
}

func TestAccess_Full(t *testing.T) {
	g := graph.NewDirectedGraph[*creds.Config]()
	source, err := creds.NewTestAssumesAllConfig(creds.SourceAssumeRole, "role/source", g)
	if err != nil {
		t.Fatal(err)
	}

	target, err := source.Assume(ctx, "arn:aws:iam::123456789012:role/target")
	if err != nil {
		t.Fatal(err)
	}

	if g.AddNode(source) == nil {
		t.Error("g.AddNode returned false, expected true")
	}
	if g.AddNode(target) == nil {
		t.Error("g.AddNode returned false, expected true")
	}

	g.AddEdge(source, target)
	g.AddEdge(target, source)

	access, err := NewTestAccess(g)
	if err != nil {
		t.Fatal(err)
	}

	access.Run(ctx, source)
	access.Run(ctx, target)

	bytes, err := g.MarshalJSON()
	if err != nil {
		t.Fatal(err)
	}

	newGraph := graph.NewDirectedGraph[*creds.Config]()
	err = newGraph.UnmarshalJSON(bytes)
	if err != nil {
		t.Fatal(err)
	}
}

func NewTestAccess(g *graph.Graph[*creds.Config]) (*Access, error) {
	return NewAccess(&NewAccessInput{
		Context: ctx,
		GlobalPluginArgs: GlobalPluginArgs{
			Debug:  false,
			Region: "us-east-1",
			Lq:     lq.NewListQueue[types.Role](),
			Graph:  g,
			Scope:  []string{testAccountId},
		},
		Path:          testPath,
		AccessRefresh: 3,
		SqsConfig: aws.Config{
			Region:      testRegion,
			Credentials: credentials.NewStaticCredentialsProvider("key", "secret", "session"),
		},
		SqsQueue: testsqsQueue,
	})
}
