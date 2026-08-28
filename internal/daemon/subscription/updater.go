package subscription

import (
	"context"
	"fmt"
	"slices"
	"sync"
	"time"

	"github.com/luynrs/justray/internal/daemon/store"
	"github.com/luynrs/justray/internal/shared/domain"
	"github.com/luynrs/justray/internal/shared/parser"
	"github.com/luynrs/justray/internal/shared/parser/protocols"
	"github.com/luynrs/justray/internal/shared/rpc"
)

func (s *Service) RefreshAll(ctx context.Context) ([]rpc.Sub, error) {
	subs, err := s.store.Subscriptions()
	if err != nil {
		return nil, err
	}

	errs := make([]error, len(subs))
	jobs := make(chan int)
	var wg sync.WaitGroup
	for range min(8, len(subs)) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range jobs {
				errs[i] = s.fill(ctx, &subs[i])
			}
		}()
	}
	for i := range subs {
		select {
		case jobs <- i:
		case <-ctx.Done():
			close(jobs)
			wg.Wait()
			return nil, ctx.Err()
		}
	}
	close(jobs)
	wg.Wait()

	out := make([]rpc.Sub, len(subs))
	updated := make([]store.Subscription, 0, len(subs))
	var failed error
	for i, err := range errs {
		// on failure subs[i] keeps its pre-refresh data
		out[i] = info(subs[i])
		if err != nil {
			failed = err
			s.log.Print(err)
			continue
		}
		updated = append(updated, subs[i])
	}

	s.storeMu.Lock()
	defer s.storeMu.Unlock()
	if err := s.merge(updated); err != nil {
		return nil, err
	}
	if failed != nil {
		return out, fmt.Errorf("%d of %d subscriptions failed, last: %w", len(subs)-len(updated), len(subs), failed)
	}
	return out, nil
}

func (s *Service) Refresh(ctx context.Context, id string) (rpc.Sub, error) {
	subs, err := s.store.Subscriptions()
	if err != nil {
		return rpc.Sub{}, err
	}
	i := slices.IndexFunc(subs, func(sub store.Subscription) bool { return sub.ID == id })
	if i < 0 {
		return rpc.Sub{}, fmt.Errorf("subscription %q not found", id)
	}
	if err := s.fill(ctx, &subs[i]); err != nil {
		return rpc.Sub{}, err
	}

	s.storeMu.Lock()
	defer s.storeMu.Unlock()
	if err := s.merge(subs[i : i+1]); err != nil {
		return rpc.Sub{}, err
	}
	return info(subs[i]), nil
}

func (s *Service) merge(updated []store.Subscription) error {
	subs, err := s.store.Subscriptions()
	if err != nil {
		return err
	}
	for _, u := range updated {
		if i := slices.IndexFunc(subs, func(x store.Subscription) bool { return x.ID == u.ID }); i >= 0 {
			subs[i] = u
		}
	}
	return s.store.Save(subs)
}

func (s *Service) fill(ctx context.Context, sub *store.Subscription) error {
	if parser.IsLink(sub.URL) {
		n, err := parser.ParseURI(sub.URL)
		if err != nil {
			return err
		}
		nodes := []domain.Node{n}
		if err := validateNodes(nodes); err != nil {
			return err
		}
		preserveIDs(nodes, sub.Nodes)
		sub.Nodes, sub.Name, sub.Traffic = nodes, n.Name, domain.Traffic{}
		sub.UpdatedAt = time.Now().UTC()
		return nil
	}

	nodes, name, traffic, err := s.fetch(ctx, sub.URL)
	if err != nil {
		return err
	}
	preserveIDs(nodes, sub.Nodes)
	sub.Nodes, sub.Traffic, sub.UpdatedAt = nodes, traffic, time.Now().UTC()
	if name != "" { // change name if it changed on server
		sub.Name = name
	}
	return nil
}

func validateNodes(nodes []domain.Node) error {
	for _, n := range nodes {
		if n.TLS != nil && n.TLS.Insecure {
			return fmt.Errorf("subscription contains an insecure node")
		}
	}
	return nil
}

func preserveIDs(nodes, old []domain.Node) {
	ids := make(map[string]string, len(old))
	for _, previous := range old {
		id := protocols.NodeID(previous)
		if _, ok := ids[id]; !ok {
			ids[id] = previous.ID
		}
	}
	for i := range nodes {
		if id, ok := ids[nodes[i].ID]; ok {
			nodes[i].ID = id
		}
	}
}
