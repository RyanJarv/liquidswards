package creds

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/RyanJarv/liquidswards/lib/graph"
	"github.com/RyanJarv/liquidswards/lib/utils"
	"github.com/google/go-cmp/cmp"
	"testing"
)

var jsonNodeBytes = []byte(`{
  "Value": {
    "Arn": "Arn:aws:iam::123456789012:role/test",
    "State": 0,
    "Region": "us-east-1",
    "Credentials": {
      "AccessKeyID": "test",
      "SecretAccessKey": "test",
      "SessionToken": "",
      "Identity": "Arn:aws:iam::123456789012:role/test",
      "CanExpire": false,
      "Expires": "0001-01-01T00:00:00Z"
    },
    "Identity": {
      "Type": 0,
      "Name": "test",
      "Arn": "Arn:aws:iam::123456789012:role/test"
    }
  },
  "Assumes": [
    "Arn:aws:iam::123456789012:role/test"
  ],
  "AssumedBy": [
    "Arn:aws:iam::123456789012:role/test"
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
	cfg, err := NewConfig(ctx, "us-east-1", Identity{
		Type: SourceProfile,
		Name: "test",
		Arn:  "Arn:aws:iam::123456789012:role/test",
	})

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
