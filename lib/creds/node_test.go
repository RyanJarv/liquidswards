package creds

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/RyanJarv/liquidswards/lib/graph"
	"github.com/RyanJarv/liquidswards/lib/utils"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/google/go-cmp/cmp"
	"testing"
)

var jsonNodeBytes = []byte(`{
  "Value": {
    "Arn": "arn:aws:iam::123456789012:role/test",
    "State": 0,
    "Region": "us-east-1",
    "Credentials": {
      "AccessKeyID": "test",
      "SecretAccessKey": "test",
      "SessionToken": "",
      "Source": "arn:aws:iam::123456789012:role/test",
      "CanExpire": false,
      "Expires": "0001-01-01T00:00:00Z"
    },
    "Source": {
      "Type": 0,
      "Name": "test",
      "Arn": "arn:aws:iam::123456789012:role/test"
    }
  },
  "Assumes": [
    "arn:aws:iam::123456789012:role/test"
  ],
  "AssumedBy": [
    "arn:aws:iam::123456789012:role/test"
  ]
}`)

func TestNode_Save(t *testing.T) {
	node, err := NewNode()
	if err != nil {
		t.Fatal(err)
	}

	got, err := json.MarshalIndent(node, "", "  ")
	if err != nil {
		t.Fatal(err)
	}

	if diff := cmp.Diff(string(got), string(jsonNodeBytes)); diff != "" {
		t.Error(diff)
	}
}

func TestNode_Load(t *testing.T) {
	node, err := NewNode()
	if err != nil {
		t.Fatal(err)
	}

	err = json.Unmarshal(jsonNodeBytes, &node)
	if err != nil {
		t.Fatal(err)
	}

	got, err := json.MarshalIndent(node, "", "  ")
	if err != nil {
		t.Fatal(err)
	}

	if diff := cmp.Diff(string(got), string(jsonNodeBytes)); diff != "" {
		t.Error(diff)
	}
}

func NewNode() (graph.Node[*Config], error) {
	ctx = utils.NewContext(context.Background())
	cfg, err := NewConfig(
		ctx,
		aws.Credentials{
			AccessKeyID:     "test",
			SecretAccessKey: "test",
			Source:          "test",
			CanExpire:       false,
		},
		"us-east-1",
		Source{
			Type: SourceProfile,
			Name: "test",
			Arn:  "arn:aws:iam::123456789012:role/test",
		},
	)

	if err != nil {
		return nil, fmt.Errorf("creating cfg %s", err)
	}

	n1 := graph.NewNode[*Config](
		graph.NewNodeInput[*Config]{
			Value: cfg,
		},
	)
	n2 := graph.NewNode[*Config](
		graph.NewNodeInput[*Config]{
			Value:     cfg,
			AssumedBy: []graph.Node[*Config]{n1},
			Assumes:   []graph.Node[*Config]{n1},
		},
	)

	return n2, err
}
