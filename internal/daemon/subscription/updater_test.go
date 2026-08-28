package subscription

import (
	"testing"

	"github.com/luynrs/justray/internal/shared/domain"
	"github.com/luynrs/justray/internal/shared/parser/protocols"
)

func TestPreserveIDs(t *testing.T) {
	old := domain.Node{ID: "persisted", Protocol: domain.Trojan, Server: "example.com", Port: 443}
	nodes := []domain.Node{old}
	nodes[0].ID = protocols.NodeID(nodes[0])
	preserveIDs(nodes, []domain.Node{old})
	if nodes[0].ID != old.ID {
		t.Fatalf("ID = %q, want %q", nodes[0].ID, old.ID)
	}
}
