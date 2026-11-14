package server

import (
	"reflect"
	"testing"
)

func TestExpandDepsPullsInDependenciesInOrder(t *testing.T) {
	deps := map[string][]string{"web": {"api"}, "api": {"db", "cache"}, "db": nil, "cache": nil, "worker": {"db"}}
	order, pulled, err := expandDeps([]string{"web", "worker"}, deps)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(order, []string{"db", "cache", "api", "web", "worker"}) {
		t.Fatalf("order = %v", order)
	}
	if !reflect.DeepEqual(pulled, []string{"db", "cache", "api"}) {
		t.Fatalf("pulled = %v", pulled)
	}
}

func TestExpandDepsRejectsCyclesAndUnknownApps(t *testing.T) {
	if _, _, err := expandDeps([]string{"a"}, map[string][]string{"a": {"b"}, "b": {"a"}}); err == nil {
		t.Fatal("cycle accepted")
	}
	if _, _, err := expandDeps([]string{"nope"}, map[string][]string{"a": nil}); err == nil {
		t.Fatal("unknown app accepted")
	}
}
