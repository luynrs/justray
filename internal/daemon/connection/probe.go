package connection

import (
	"context"
	"fmt"

	"github.com/luynrs/justray/internal/daemon/store"
	"github.com/luynrs/justray/internal/shared/domain"
	"github.com/luynrs/justray/internal/shared/rpc"
)

func (s *Service) Nodes(subs []store.Subscription) []rpc.Node {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := []rpc.Node{} // never nil: the TUI tells "no nodes" from "not loaded yet"
	for _, sub := range subs {
		for _, n := range sub.Nodes {
			ref := domain.NodeRef{SubscriptionID: sub.ID, NodeID: n.ID}
			item := rpc.Node{
				ID: n.ID, Name: n.Name, Protocol: string(n.Protocol),
				Server: n.Server, Port: n.Port,
				Sub: sub.ID,
			}
			if p, ok := s.probes[ref]; ok {
				item.Probed, item.Alive, item.MS = true, p.Alive, p.MS
			}
			out = append(out, item)
		}
	}
	return out
}

// Probe pings one node if id is set, else every node in sub, else all of them.
func (s *Service) Probe(ctx context.Context, subs []store.Subscription, sub, id string) ([]rpc.Node, error) {
	live := map[domain.NodeRef]bool{}
	var refs []domain.NodeRef
	var targets []domain.Node
	for _, x := range subs {
		for _, n := range x.Nodes {
			ref := domain.NodeRef{SubscriptionID: x.ID, NodeID: n.ID}
			live[ref] = true
			if (sub == "" || x.ID == sub) && (id == "" || n.ID == id) {
				refs = append(refs, ref)
				targets = append(targets, n)
			}
		}
	}
	if len(targets) == 0 {
		return nil, fmt.Errorf("nothing to probe")
	}

	results, err := s.probeAll(ctx, targets, s.current(), rpc.EngineLog(s.dir))
	if err != nil {
		return nil, err
	}

	s.mu.Lock()
	for _, ref := range refs {
		if result, ok := results[ref.NodeID]; ok {
			s.probes[ref] = result
		}
	}
	for ref := range s.probes {
		if !live[ref] {
			delete(s.probes, ref)
		}
	}
	s.mu.Unlock()
	return s.Nodes(subs), nil
}
