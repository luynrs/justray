package cli

import (
	"errors"
	"testing"

	"github.com/spf13/cobra"

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
		{"Frankfurt am Main", "Frankfurt am Main"},
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

	if _, err := match("zzz", "node", items, idName); !errors.Is(err, errNotFound) {
		t.Errorf("match(zzz): want notFoundError, got %v", err)
	}
	if _, err := match("frankfurt", "node", items, idName); errors.Is(err, errNotFound) {
		t.Errorf("match(frankfurt): want ambiguity error, got %v", err)
	}
}

func TestLookup(t *testing.T) {
	nodes := []ipc.Node{
		{NodeID: "node1", SubscriptionID: "sub1", Name: "Node 1"},
		{NodeID: "node2", SubscriptionID: "sub2", Name: "Node 2"},
	}
	if n := lookup(domain.NodeRef{SubscriptionID: "sub1", NodeID: "node1"}, nodes); n.Name != "Node 1" {
		t.Fatalf("lookup = %+v, want Node 1", n)
	}
	if n := lookup(domain.NodeRef{SubscriptionID: "", NodeID: "node2"}, nodes); n.Name != "Node 2" {
		t.Fatalf("lookup without sub = %+v, want Node 2", n)
	}
	if n := lookup(domain.NodeRef{SubscriptionID: "sub1", NodeID: "unknown"}, nodes); n.NodeID != "" {
		t.Fatalf("lookup unknown = %+v, want empty", n)
	}
}

func TestJSONFlags(t *testing.T) {
	if f := statusCmd.Flags().Lookup("json"); f == nil {
		t.Error("statusCmd missing --json flag")
	}
	if f := subListCmd.Flags().Lookup("json"); f == nil {
		t.Error("subListCmd missing --json flag")
	}
}

func TestMatchRegex(t *testing.T) {
	items := []ipc.Node{{NodeID: "1", Name: "🇩🇪 Castor  "}, {NodeID: "2", Name: "🇩🇪 Frankfurt"}, {NodeID: "3", Name: "🇬🇧 London"}}
	idName := func(n ipc.Node) (string, string) { return n.NodeID, n.Name }
	for pattern, want := range map[string]string{`^🇬🇧`: "3", `castor$`: "1", `Castor$`: "1"} {
		if got, err := match(pattern, "node", items, idName, true); err != nil || got.NodeID != want {
			t.Errorf("match(%q, regex) = %q, %v; want %q", pattern, got.NodeID, err, want)
		}
	}
	for _, pattern := range []string{`LONDON`, `zzz`, `^🇩🇪`, `[`} {
		if _, err := match(pattern, "node", items, idName, true); err == nil {
			t.Errorf("match(%q, regex): want error", pattern)
		}
	}
	for _, cmd := range []*cobra.Command{upCmd, probeCmd} {
		if f := cmd.Flags().Lookup("regex"); f == nil || f.Shorthand != "r" {
			t.Errorf("%s missing -r/--regex flag", cmd.Name())
		}
	}
}
