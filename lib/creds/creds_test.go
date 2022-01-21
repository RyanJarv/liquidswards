package creds

import (
	"bytes"
	"context"
	"encoding/json"
	"github.com/RyanJarv/liquidswards/lib/graph"
	"github.com/RyanJarv/liquidswards/lib/utils"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/google/go-cmp/cmp"
	"testing"
)

var ctx = utils.NewContext(context.Background())

// Unsure why this is failing
//func TestConfig_Assume(t *testing.T) {
//	ctx := utils.NewContext(context.Background())
//	g := graph.NewDirectedGraph[*Config]()
//	source, err := NewTestAssumesAllConfig(SourceProfile, "user/profile-a", g)
//	if err != nil {
//		t.Fatal(err)
//	}
//
//	devops, err := source.Assume(ctx, "arn:aws:iam::123456789012:user/test") // Just needs to parse
//	if err != nil {
//		t.Fatal(err)
//	}
//
//	if want := "arn:aws:iam::123456789012:user/profile-a"; source.Arn() != want {
//		t.Errorf("source.Arn(): got %s, want %s", source.Arn(), want)
//	}
//
//	if want := "arn:aws:iam::123456789012:user/test"; devops.Arn() != want {
//		t.Errorf("got %s want %s", devops.Arn(), want)
//	}
//
//	if want := ActiveState; devops.State() != ActiveState {
//		t.Errorf("got %q want %q", devops.State(), want)
//	}
//
//	got, err := devops.Config().Credentials.Retrieve(ctx)
//	if err != nil {
//		t.Fatal(err)
//	}
//
//	want := utils.NewTestCreds(true, "arn:aws:iam::123456789012:user/profile-a")
//	if msg := cmp.Diff(got, want); msg != "" {
//		t.Errorf(msg)
//	}
//}

func TestConfig_Refresh(t *testing.T) {
	g := graph.NewDirectedGraph[*Config]()
	sourceCfg := utils.Must(NewTestAssumesAllConfig(SourceProfile, "user/profile-a-arn", g))
	expiredCfg := utils.Must(NewTestAssumesAllConfig(SourceAssumeRole, "role/target-profile-arn", g))

	g.AddNode(sourceCfg)
	g.AddNode(expiredCfg)
	g.AddEdge(sourceCfg, expiredCfg)

	refreshedCfg, err := expiredCfg.Refresh(ctx)
	if err != nil {
		t.Fatal(err)
	}

	got, err := refreshedCfg.Config().Credentials.Retrieve(ctx)
	if err != nil {
		t.Fatal(err)
	}

	want := utils.NewTestCreds(true, "arn:aws:iam::123456789012:user/profile-a-arn")
	if msg := cmp.Diff(want, got); msg != "" {
		t.Errorf(msg)
	}
}

var configJson = bytes.Trim([]byte(`
{
  "Arn": "arn:aws:iam::123456789012:user/test",
  "State": 0,
  "Region": "",
  "Credentials": {
    "AccessKeyID": "test",
    "SecretAccessKey": "test",
    "SessionToken": "test",
    "Source": "arn:aws:iam::123456789012:user/test",
    "CanExpire": false,
    "Expires": "0001-01-01T00:00:00Z"
  },
  "Source": {
    "Type": 1,
    "Name": "arn:aws:iam::123456789012:user/test",
    "Arn": "arn:aws:iam::123456789012:user/test"
  }
}
`), " \t\n")

func TestConfig_Marshal(t *testing.T) {
	cfg := &Config{
		source: Source{
			Type: SourceAssumeRole,
			Name: "arn:aws:iam::123456789012:user/test",
			Arn:  "arn:aws:iam::123456789012:user/test",
		},
		cfg: aws.Config{Credentials: credentials.StaticCredentialsProvider{
			Value: aws.Credentials{
				AccessKeyID:     "test",
				SecretAccessKey: "test",
				SessionToken:    "test",
				Source:          "arn:aws:iam::123456789012:user/test",
			},
		}},
		state: ActiveState,
	}

	got, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		t.Fatal(err)
	}

	if diff := cmp.Diff(string(got), string(configJson)); diff != "" {
		t.Error(diff)
	}
}

func TestConfig_Unmarshal(t *testing.T) {
	cfg := Config{}

	err := json.Unmarshal(configJson, &cfg)
	if err != nil {
		t.Fatal(err)
	}

	got, err := json.MarshalIndent(&cfg, "", "  ")
	if err != nil {
		t.Fatal(err)
	}

	if cfg.source.Arn == "" {
		t.Error("cfg.source.Arn is empty")
	}

	// TODO: Figure out why go-cmp isn't working here
	if diff := cmp.Diff(got, configJson); diff != "" {
		t.Error(diff)
	}
}
