package cli

import (
	"errors"
	"strings"
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
	items := []ipc.Node{
		{NodeID: "11111111", Name: "🇩🇪 Castor"},
		{NodeID: "22222222", Name: "🇩🇪 Frankfurt gRPC"},
		{NodeID: "33333333", Name: "🇬🇧 London"},
		{NodeID: "44444444", Name: "🇨🇭 Zurich"},
	}
	idName := func(n ipc.Node) (string, string) { return n.NodeID, n.Name }

	for _, tc := range []struct {
		pattern, want string
	}{
		{`^🇨🇭`, "🇨🇭 Zurich"},
		{`(?i)castor`, "🇩🇪 Castor"},
		{`^🇩🇪 Frankfurt`, "🇩🇪 Frankfurt gRPC"},
	} {
		got, err := matchRegex(tc.pattern, "node", items, idName)
		if err != nil {
			t.Errorf("matchRegex(%q): %v", tc.pattern, err)
		} else if got.Name != tc.want {
			t.Errorf("matchRegex(%q) = %q, want %q", tc.pattern, got.Name, tc.want)
		}
	}

	if _, err := matchRegex(`^🇩🇪`, "node", items, idName); err == nil || !strings.Contains(err.Error(), "ambiguous") {
		t.Errorf("matchRegex(^🇩🇪): want ambiguous error, got %v", err)
	}
	if _, err := matchRegex(`zzz`, "node", items, idName); !errors.Is(err, errNotFound) {
		t.Errorf("matchRegex(zzz): want notFoundError, got %v", err)
	}
	if _, err := matchRegex(`[`, "node", items, idName); err == nil || !strings.Contains(err.Error(), "invalid") {
		t.Errorf("matchRegex([): want invalid regex error, got %v", err)
	}
}

func TestUpRegexFlag(t *testing.T) {
	f := upCmd.Flags().Lookup("regex")
	if f == nil {
		t.Fatal("upCmd missing --regex flag")
	}
	if f.Shorthand != "r" {
		t.Errorf("upCmd --regex shorthand = %q, want %q", f.Shorthand, "r")
	}
}
