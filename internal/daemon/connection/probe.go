package connection

import (
	"context"
	"fmt"
	"maps"

	"github.com/luynrs/justray/internal/shared/domain"
	"github.com/luynrs/justray/internal/shared/rpc"
)

func (s *Service) Nodes() ([]rpc.Node, error) {
	subs, err := s.store.Subscriptions()
	if err != nil {
		return nil, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	out := []rpc.Node{} // never nil: the TUI tells "no nodes" from "not loaded yet"
	for _, sub := range subs {
		for _, n := range sub.Nodes {
			item := rpc.Node{
				ID: n.ID, Name: n.Name, Protocol: string(n.Protocol),
				Server: n.Server, Port: n.Port,
				Sub: sub.ID,
			}
			if p, ok := s.probes[n.ID]; ok {
				item.Probed, item.Alive, item.MS = true, p.Alive, p.MS
			}
			out = append(out, item)
		}
	}
	return out, nil
}

// Probe pings one node if id is set, else every node in sub, else all of them.
func (s *Service) Probe(ctx context.Context, sub, id string) ([]rpc.Node, error) {
	subs, err := s.store.Subscriptions()
	if err != nil {
		return nil, err
	}

	var targets []domain.Node
	for _, x := range subs {
		if sub != "" && x.ID != sub {
			continue
		}
		for _, n := range x.Nodes {
			if id == "" || n.ID == id {
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

	live := map[string]bool{}
	for _, x := range subs {
		for _, n := range x.Nodes {
			live[n.ID] = true
		}
	}

	s.mu.Lock()
	maps.Copy(s.probes, results)
	for id := range s.probes {
		if !live[id] {
			delete(s.probes, id)
		}
	}
	s.mu.Unlock()
	return s.Nodes()
}
