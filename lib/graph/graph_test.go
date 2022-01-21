// Most of the graph tests are in lib/creds/graph_test.go because that includes the types we actually use in this repo.
package graph

import (
	"sync"
	"testing"
)

type V struct{}

func (v V) ID() string                       { return "test" }
func (v V) SetGraph(_ interface{})           { return }
func (v V) UnmarshalJSON(bytes []byte) error { return nil }
func (v V) MarshalJSON() ([]byte, error)     { return []byte{}, nil }

func TestNewDirectedGraph(t *testing.T) {
	_ = Graph[V]{
		nodes: map[string]Node[V]{},
		m:     &sync.Mutex{},
	}
	g := NewDirectedGraph[V]()
	if g.m == nil {
		t.Error("graph.m is nil")
	}
	if g.nodes == nil {
		t.Error("graph.nodes is nil")
	}
}
