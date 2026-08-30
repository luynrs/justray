package subscription

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/luynrs/justray/internal/daemon/store"
	"github.com/luynrs/justray/internal/shared/domain"
	"github.com/luynrs/justray/internal/shared/parser"
	"github.com/luynrs/justray/internal/shared/rpc"
)

func (s *Service) RefreshAll(ctx context.Context, subs []store.Subscription, refresh func(context.Context, store.Subscription) (store.Subscription, error)) ([]rpc.Sub, []store.Subscription, error) {
	errs := make([]error, len(subs))
	jobs := make(chan int)
	var wg sync.WaitGroup
	for range min(8, len(subs)) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range jobs {
				subs[i], errs[i] = refresh(ctx, subs[i])
			}
		}()
	}
	for i := range subs {
		select {
		case jobs <- i:
		case <-ctx.Done():
			close(jobs)
			wg.Wait()
			return nil, nil, ctx.Err()
		}
	}
	close(jobs)
	wg.Wait()

	out := make([]rpc.Sub, len(subs))
	updated := make([]store.Subscription, 0, len(subs))
	var failed error
	for i, err := range errs {
		// on failure subs[i] keeps its pre-refresh data
		out[i] = Info(subs[i])
		if err != nil {
			failed = err
			s.log.Print(err)
			continue
		}
		updated = append(updated, subs[i])
	}

	if failed != nil {
		return out, updated, fmt.Errorf("%d of %d subscriptions failed, last: %w", len(subs)-len(updated), len(subs), failed)
	}
	return out, updated, nil
}

func (s *Service) Refresh(ctx context.Context, sub store.Subscription) (store.Subscription, error) {
	if err := s.fill(ctx, &sub); err != nil {
		return sub, err
	}
	return sub, nil
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
		sub.Nodes, sub.Name, sub.Traffic = nodes, n.Name, domain.Traffic{}
		sub.UpdatedAt = time.Now().UTC()
		return nil
	}

	nodes, name, traffic, err := s.fetch(ctx, sub.URL)
	if err != nil {
		return err
	}
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
