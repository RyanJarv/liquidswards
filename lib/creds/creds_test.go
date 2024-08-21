package creds

import (
	"bytes"
	"context"
	"encoding/json"
	"github.com/RyanJarv/liquidswards/lib/graph"
	"github.com/RyanJarv/liquidswards/lib/utils"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/sts"
	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"testing"
)

var ctx = utils.NewContext(context.Background())

// TestConfig_Assume ensures that refreshing a target calls AssumeRole on the source.
func TestConfig_Refresh(t *testing.T) {
	wantSourceAssumeCalls := []sts.AssumeRoleInput{
		{
			RoleArn:         aws.String("arn:aws:iam::123456789012:role/target"),
			RoleSessionName: aws.String("liquidswards"),
			DurationSeconds: aws.Int32(900),
		},
	}

	g := graph.NewDirectedGraph[*Config]()
	sourceCfg, sourceClient := utils.Must2(NewTestAssumesAllConfig(SourceProfile, "user/source", g))
	targetCfg, _ := utils.Must2(NewTestAssumesAllConfig(SourceAssumeRole, "role/target", g))

	g.AddNode(sourceCfg)
	g.AddNode(targetCfg)
	g.AddEdge(sourceCfg, targetCfg)

	gotCreds, err := targetCfg.Refresh(ctx)
	if err != nil {
		t.Fatal(err)
	}

	wantCreds := utils.NewTestCreds(true, "AssumeRoleProvider")
	if msg := cmp.Diff(gotCreds, wantCreds); msg != "" {
		t.Errorf("target Refresh() creds mismatch (-got +want):\n%s", msg)
	}

	if msg := cmp.Diff(sourceClient.Calls, wantSourceAssumeCalls, cmp.Options{
		cmpopts.IgnoreUnexported(sts.AssumeRoleInput{}),
	}); msg != "" {
		t.Errorf("source AssumeRole() call mismatch (-got +want):\n%s", msg)
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
    "Identity": "arn:aws:iam::123456789012:user/test",
    "CanExpire": false,
    "Expires": "0001-01-01T00:00:00Z"
  },
  "Identity": {
    "Type": 1,
    "Name": "arn:aws:iam::123456789012:user/test",
    "Arn": "arn:aws:iam::123456789012:user/test"
  }
}
`), " \t\n")

func TestConfig_Marshal(t *testing.T) {
	cfg := &Config{
		Identity: Identity{
			Type: SourceAssumeRole,
			Name: "arn:aws:iam::123456789012:user/test",
			Arn:  "arn:aws:iam::123456789012:user/test",
		},
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

	if cfg.Identity.Arn == "" {
		t.Error("cfg.source.Arn is empty")
	}

	// TODO: Figure out why go-cmp isn't working here
	if diff := cmp.Diff(got, configJson); diff != "" {
		t.Errorf("cfg mismatch (-got +want):\n%s", diff)
	}
}
