package creds

import (
	"encoding/json"
	"github.com/RyanJarv/liquidswards/lib/graph"
	"github.com/RyanJarv/liquidswards/lib/utils"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/google/go-cmp/cmp"
	"strings"
	"testing"
)

var (
	cfg = &Config{
		source: Source{
			Type: SourceAssumeRole,
			Name: "arn:aws:iam::123456789012:user/test",
			Arn:  "arn:aws:iam::123456789012:user/test",
		},
		cfg: aws.Config{Credentials: credentials.NewStaticCredentialsProvider(
			"accesskey",
			"secretaccesskey",
			"sessiontoken",
		)},
		state: ActiveState,
	}
)

var transformJSON = cmp.FilterValues(
	func(x, y []byte) bool {
		return json.Valid(x) && json.Valid(y)
	},
	cmp.Transformer("ParseJSON", func(in []byte) (out interface{}) {
		if err := json.Unmarshal(in, &out); err != nil {
			panic(err) // should never occur given previous filter to ensure valid JSON
		}
		return out
	}),
)

func TestGraph_AddNode(t *testing.T) {
	g := graph.NewDirectedGraph[*Config]()
	cfg := utils.Must(NewTestAssumesAllConfig(SourceProfile, "user/profile-a-arn", g))

	if g.AddNode(cfg) == nil {
		t.Error("g.AddNode returned false, expected true")
	}

	if got := len(g.Nodes()); got != 1 {
		t.Errorf("len(g.Nodes()): got %d, want 1", got)
	}

	want := "arn:aws:iam::123456789012:user/profile-a-arn"
	var names []string
	var node graph.Node[*Config]
	for k, v := range g.Nodes() {
		names = append(names, k)
		if k == want {
			node = v
		}
	}
	if node == nil {
		t.Fatalf("no node named %s found in: '%s'", want, strings.Join(names, ", "))
	}

	if got := node.Value().Arn(); got != want {
		t.Errorf("config arn: got %s, want: %s", got, want)
	}

}

func TestGraph_SaveNode(t *testing.T) {
	g := graph.NewDirectedGraph[*Config]()
	node := g.AddNode(cfg)
	if node == nil {
		t.Error("g.AddNode returned false, expected true")
	}

	got, err := json.MarshalIndent(node, "", "  ")
	if err != nil {
		t.Fatal(err)
	}

	want := []byte(`{
  "Value": {
    "Arn": "arn:aws:iam::123456789012:user/test",
    "State": 0,
    "Region": "",
    "Credentials": {
      "AccessKeyID": "accesskey",
      "SecretAccessKey": "secretaccesskey",
      "SessionToken": "sessiontoken",
      "Source": "StaticCredentials",
      "CanExpire": false,
      "Expires": "0001-01-01T00:00:00Z"
    },
    "Source": {
      "Type": 1,
      "Name": "arn:aws:iam::123456789012:user/test",
      "Arn": "arn:aws:iam::123456789012:user/test"
    }
  },
  "Assumes": null,
  "AssumedBy": null
}`)
	if diff := cmp.Diff(got, want, transformJSON); diff != "" {
		t.Error(diff)
	}
}

var JsonBytes = []byte(`{
  "arn:aws:iam::123456789012:user/test": {
    "Value": {
      "Arn": "arn:aws:iam::123456789012:user/test",
      "State": 0,
      "Region": "us-east-1",
      "Credentials": {
        "AccessKeyID": "test",
        "SecretAccessKey": "test",
        "SessionToken": "test",
        "Source": "arn:aws:iam::123456789012:user/test",
        "CanExpire": true,
        "Expires": "2022-05-25T17:00:00-07:00"
      },
      "Source": {
        "Type": 1,
        "Name": "arn:aws:iam::123456789012:user/test",
        "Arn": "arn:aws:iam::123456789012:user/test"
      }
    },
    "Assumes": [
      "arn:aws:iam::123456789012:user/test"
    ],
    "AssumedBy": [
      "arn:aws:iam::123456789012:user/test"
    ]
  },
  "arn:aws:iam::123456789012:user/test": {
    "Value": {
      "Arn": "arn:aws:iam::123456789012:user/test",
      "State": 0,
      "Region": "us-east-1",
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
    },
    "Assumes": [
      "arn:aws:iam::123456789012:user/test"
    ],
    "AssumedBy": [
      "arn:aws:iam::123456789012:user/test"
    ]
  }
}`)

func TestGraph_Save(t *testing.T) {
	jsonBytes := []byte(`{
  "arn:aws:iam::123456789012:role/test": {
    "Value": {
      "Arn": "arn:aws:iam::123456789012:role/test",
      "State": 0,
      "Region": "us-east-1",
      "Credentials": {
        "AccessKeyID": "test",
        "SecretAccessKey": "test",
        "SessionToken": "test",
        "Source": "arn:aws:iam::123456789012:user/test",
        "CanExpire": true,
        "Expires": "2022-05-25T17:00:00-07:00"
      },
      "Source": {
        "Type": 1,
        "Name": "arn:aws:iam::123456789012:role/test",
        "Arn": "arn:aws:iam::123456789012:role/test"
      }
    },
    "Assumes": [
      "arn:aws:iam::123456789012:user/test"
    ],
    "AssumedBy": [
      "arn:aws:iam::123456789012:user/test"
    ]
  },
  "arn:aws:iam::123456789012:user/test": {
    "Value": {
      "Arn": "arn:aws:iam::123456789012:user/test",
      "State": 0,
      "Region": "us-east-1",
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
    },
    "Assumes": [
      "arn:aws:iam::123456789012:role/test"
    ],
    "AssumedBy": [
      "arn:aws:iam::123456789012:role/test"
    ]
  }
}`)
	g, err := NewFilledGraph(t)
	got, err := json.MarshalIndent(g, "", "  ")
	if err != nil {
		t.Fatal(err)
	}

	if diff := cmp.Diff(got, jsonBytes, transformJSON); diff != "" {
		t.Error(diff)
	}
}

// Fails because we're sharing the expected results across tests and there is two keys that are the same in
// the expected results (just because all the mocked methods return the same thing currently). Need to
// figure out a better way to do this.
func TestGraph_Load(t *testing.T) {
	jsonBytes := []byte(`{
  "arn:aws:iam::123456789012:user/test": {
    "Value": {
      "Arn": "arn:aws:iam::123456789012:user/test",
      "State": 0,
      "Region": "us-east-1",
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
    },
    "Assumes": [
      "arn:aws:iam::123456789012:user/test"
    ],
    "AssumedBy": [
      "arn:aws:iam::123456789012:user/test"
    ]
  }
}`)

	g, err := NewFilledGraph(t)
	err = json.Unmarshal(JsonBytes, &g)
	if err != nil {
		t.Fatal(err)
	}

	got, err := json.MarshalIndent(g, "", "  ")
	if err != nil {
		t.Fatal(err)
	}

	if diff := cmp.Diff(got, jsonBytes, transformJSON); diff != "" {
		t.Error(diff)
	}
}

func NewFilledGraph(t *testing.T) (*graph.Graph[*Config], error) {
	g := graph.NewDirectedGraph[*Config]()
	source, err := NewTestAssumesAllConfig(SourceAssumeRole, "user/test", g)
	if err != nil {
		t.Fatal(err)
	}

	target, err := source.Assume(ctx, "arn:aws:iam::123456789012:role/test")
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
	return g, err
}

func TestGraph_SaveCyclic(t *testing.T) {
	g := graph.NewDirectedGraph[*Config]()
	source, err := NewTestAssumesAllConfig(SourceAssumeRole, "user/test", g)
	if err != nil {
		t.Fatal(err)
	}

	if g.AddNode(source) == nil {
		t.Error("g.AddNode returned false, expected true")
	}
	target, err := source.Assume(ctx, "arn:aws:iam::123456789012:test/test")
	if err != nil {
		t.Fatal(err)
	}

	if g.AddNode(target) == nil {
		t.Error("g.AddNode returned false, expected true")
	}

	g.AddEdge(source, target)
	g.AddEdge(target, source)

	bytes, err := g.MarshalJSON()
	if err != nil {
		t.Fatal(err)
	}

	err = g.UnmarshalJSON(bytes)
	if err != nil {
		t.Fatal(err)
	}
}
