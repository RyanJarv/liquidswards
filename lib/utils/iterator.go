package utils

import (
	"sync"
)

type Identifiable interface {
	Id() string
}

func NewIterator[T Identifiable]() *Iterator[T] {
	return &Iterator[T]{
		Locker: &sync.Mutex{},
		Items:  &sync.Map{},
		items:  make([]T, 0, 100),
		funcs:  make([]func(T), 0, 100),
	}
}

type Iterator[T Identifiable] struct {
	sync.Locker
	Items *sync.Map
	funcs []func(T)
	items []T
}

func (g *Iterator[T]) Walk(f func(T)) {
	// In the case both Walk and Add is called at the same time ensure we have a consistent
	// snapshot of both funcs and items since accessing these concurrently would lead to the
	// function processing the role in both this function and in Add.
	g.Lock()

	// Add f to list of funcs to ensure it is run with items added in future calls to Add.
	g.funcs = append(g.funcs, f)

	// This ensures f is run across all existing items. It copies the current item slice, since
	// slices are just references to the underlying array this should be fast.
	roles := make([]T, len(g.items))
	copy(roles, g.items)
	g.Unlock()

	// For each role in our copy of the role slice, call it our current func.
	for _, role := range roles {
		f(role)
	}

}

func (g *Iterator[T]) Add(item T) bool {
	// Avoid adding duplicate items, Items is a sync.Map which handles concurrent access.
	if _, loaded := g.Items.LoadOrStore(item.Id(), item); loaded {
		return false
	}

	// Similar to Walk method, but reversed.
	g.Lock()
	// Ensure this item is called by future calls to Walk.
	g.items = append(g.items, item)

	// Ensure this item is called by previous calls to Walk.
	funcs := make([]func(role T), len(g.funcs))
	copy(funcs, g.funcs)
	g.Unlock()

	// For each func in our copy of the func slice, call it our current item.
	for _, f := range funcs {
		f(item)
	}

	return true
}
