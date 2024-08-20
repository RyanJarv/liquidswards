package plugins

import (
	"context"
	"github.com/RyanJarv/liquidswards/lib/creds"
	"github.com/RyanJarv/liquidswards/lib/graph"
	"github.com/RyanJarv/liquidswards/lib/types"
	"github.com/RyanJarv/liquidswards/lib/utils"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/sqs"
	sqsTypes "github.com/aws/aws-sdk-go-v2/service/sqs/types"
	"github.com/aws/smithy-go/middleware"
	"github.com/google/go-cmp/cmp"
	"strings"
	"testing"
	"time"
)

const (
	testSqsQueue  = "https://test-sqs-queue.loca"
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

	access.Run(ctx)

	want := "active"
	if got := cfg.State(); got != creds.ActiveState {
		t.Errorf("got %q, want %q", got, want)
	}
	cfg.SetState(creds.RefreshingState)
}

type MockGetNode []string

func (m *MockGetNode) GetNode(k string) (graph.Node[*creds.Config], bool) {
	*m = append(*m, k)
	return nil, false
}

// TODO: Clean this up
func TestAccess_RunSqsClient(t *testing.T) {
	access := &Sqs{
		SqsQueue: testSqsQueue,
		GlobalPluginArgs: types.GlobalPluginArgs{
			Region:           "us-east-1",
			PrimaryAwsConfig: aws.Config{Region: "us-east-1"},
		},
		cfgs: map[string][]chan int{
			"role-a": {make(chan int, 10), make(chan int, 10)},
			"role-b": {make(chan int, 10)},
			"role-c": {},
		},
	}

	runCtx, cancelFunc := ctx.WithCancel()
	ch := make(chan string, 0)

	got := MockGetNode{}
	want := MockGetNode{
		"arn:aws:iam::123456789012:role/role-a",
		"arn:aws:iam::123456789012:role/role-b",
		"arn:aws:iam::123456789012:role/role-c",
	}

	go access.RunSqsClient(runCtx, func(ctx context.Context, params *sqs.ReceiveMessageInput, optFns ...func(*sqs.Options)) (*sqs.ReceiveMessageOutput, error) {
		return &sqs.ReceiveMessageOutput{
			Messages: []sqsTypes.Message{
				{Body: revokeSessMsg(strings.Split(<-ch, "/")[1])},
			},
			ResultMetadata: middleware.Metadata{},
		}, nil
	}, func(ctx context.Context, params *sqs.DeleteMessageInput, optFns ...func(*sqs.Options)) (*sqs.DeleteMessageOutput, error) {
		return &sqs.DeleteMessageOutput{}, nil
	}, got.GetNode)

	for _, arn := range want {
		ch <- arn
	}

	time.Sleep(1 * time.Second)
	cancelFunc()

	if diff := cmp.Diff(got, want); diff != "" {
		t.Errorf("got != want, -got +want:\n%s", diff)
	}
}

func revokeSessMsg(name string) *string {
	return aws.String(`{
	  "version": "0",
	  "id": "b0f351e0-8ee9-dc51-0f64-682db2c0e8fd",
	  "detail-type": "AWS API Call via CloudTrail",
	  "source": "aws.iam",
	  "account": "123456789012",
	  "time": "2022-02-16T01:36:00Z",
	  "detail": {
		"eventName": "PutRolePolicy",
		"requestParameters": {
		  "roleName": "` + name + `",
		  "policyName": "AWSRevokeOlderSessions",
		  "policyDocument": "{\"Version\":\"2012-10-17\",\"Statement\":[{\"Effect\":\"Deny\",\"Action\":[\"*\"],\"Resource\":[\"*\"],\"Condition\":{\"DateLessThan\":{\"aws:TokenIssueTime\":\"2022-02-16T01:36:00.138Z\"}}}]}"
		},
		"recipientAccountId": "123456789012"
	  }
	}`)
}
func TestAccess_Full(t *testing.T) {
	t.Skip("Requires AWS credentials")

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

	access.Run(ctx)

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

func NewTestAccess(g *graph.Graph[*creds.Config]) (*Sqs, error) {
	return &Sqs{
		GlobalPluginArgs: types.GlobalPluginArgs{
			Region:     "us-east-1",
			Access:     utils.Iterator[*creds.Config]{},
			FoundRoles: utils.Iterator[types.Role]{},
			Graph:      g,
			Scope:      []string{testAccountId},
			ProgramDir: testPath,
			PrimaryAwsConfig: aws.Config{
				Region:      testRegion,
				Credentials: credentials.NewStaticCredentialsProvider("key", "secret", "session"),
			},
		},
		SqsQueue: *sqsQueue,
		cfgs:     map[string][]chan int{},
	}, nil
}
