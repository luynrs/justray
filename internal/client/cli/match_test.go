package cli

import (
	"errors"
	"testing"

	"github.com/luynrs/justray/internal/domain"
	"github.com/luynrs/justray/internal/ipc"
)

func TestMatch(t *testing.T) {
	type item struct{ id, name string }
	items := []item{
		{"74df0a1f", "Frankfurt am Main"},
		{"9e96741d", "Frankfurt am Main gRPC"},
		{"7e022a3a", "Prague"},
	}
	idName := func(i item) (string, string) { return i.id, i.name }

	for _, tc := range []struct {
		key, want string
	}{
		{"7e022a3a", "Prague"},
		{"7E022A3A", "Prague"},
		{"7E02", "Prague"},
		{"prague", "Prague"},
		{"PRAGUE", "Prague"},
		{"grpc", "Frankfurt am Main gRPC"},
	} {
		got, err := match(tc.key, "node", items, idName)
		if err != nil {
			t.Errorf("match(%q): %v", tc.key, err)
		} else if got.name != tc.want {
			t.Errorf("match(%q) = %q, want %q", tc.key, got.name, tc.want)
		}
	}

	dupes := []item{{"4ddbbffc", "Renew your plan"}, {"4ddbbffc", "t.me/bot"}}
	if got, err := match("4ddbbffc", "node", dupes, idName); err != nil || got.name != "Renew your plan" {
		t.Errorf("shared id: got %q, %v; want the first hit, nil", got.name, err)
	}

	var nfe notFoundError
	if _, err := match("zzz", "node", items, idName); !errors.As(err, &nfe) {
		t.Errorf("match(zzz): want notFoundError, got %v", err)
	}
	if _, err := match("frankfurt", "node", items, idName); errors.As(err, &nfe) {
		t.Errorf("match(frankfurt): want ambiguity error, got %v", err)
	}
}

func TestLookupNode(t *testing.T) {
	a := &app{}
	nodes := []ipc.Node{
		{ID: "node1", Sub: "sub1", Name: "Node 1"},
		{ID: "node2", Sub: "sub2", Name: "Node 2"},
	}
	if n := a.lookupNode(domain.NodeRef{SubscriptionID: "sub1", NodeID: "node1"}, nodes); n.Name != "Node 1" {
		t.Fatalf("lookupNode = %+v, want Node 1", n)
	}
	if n := a.lookupNode(domain.NodeRef{SubscriptionID: "", NodeID: "node2"}, nodes); n.Name != "Node 2" {
		t.Fatalf("lookupNode without sub = %+v, want Node 2", n)
	}
	if n := a.lookupNode(domain.NodeRef{SubscriptionID: "sub1", NodeID: "unknown"}, nodes); n.ID != "" {
		t.Fatalf("lookupNode unknown = %+v, want empty", n)
	}
}
