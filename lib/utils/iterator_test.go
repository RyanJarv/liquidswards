package utils

import (
	"github.com/golang/mock/gomock"
	"reflect"
	"sort"
	"sync"
	"testing"
)

type MockItem struct {
	id string
}

func (m MockItem) Id() string {
	return m.id
}

func TestRoleIterator_Iter_Callback(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	tests := []struct {
		name         string
		RoleIterator *Iterator[MockItem]
		send         []MockItem
		want         []string
	}{
		{
			name: "iterates over role list",
			RoleIterator: &Iterator[MockItem]{
				Locker: &sync.Mutex{},
				Items:  &sync.Map{},
				items:  make([]MockItem, 0, 100),
				funcs:  make([]func(MockItem), 0, 100),
			},
			send: []MockItem{
				{id: "1"},
				{id: "2"},
			},
			want: []string{"1", "2"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var got1 []string
			var got2 []string

			// We call add once within both walk functions to ensure recursion is working as expected.
			tt.want = append(tt.want, "walk1", "walk2")

			// Run two similar walk functions to check that the results are the same.
			tt.RoleIterator.Walk(func(item MockItem) {
				got1 = append(got1, item.Id())

				// Only unique ids are added, which prevents infinite recursion here.
				tt.RoleIterator.Add(MockItem{"walk1"})
			})

			tt.RoleIterator.Walk(func(item MockItem) {
				got2 = append(got2, item.Id())
				tt.RoleIterator.Add(MockItem{"walk2"})
			})

			// Each walk function should be called once for every role added here.
			for _, role := range tt.send {
				tt.RoleIterator.Add(role)
			}

			// Ensure both got1 and want are sorted, because we don't particularly care about the execution order.
			sort.Strings(got1)
			sort.Strings(got2)
			sort.Strings(tt.want)

			// Ensure first walk returns the expected value
			if !reflect.DeepEqual(got1, tt.want) {
				t.Errorf("Iter() = %v, want %v", got1, tt.want)
			}

			// Ensure both walk calls return the same values.
			if !reflect.DeepEqual(got1, got2) {
				t.Errorf("got1 != got2, %v = %v", got1, got2)
			}
		})
	}
}
