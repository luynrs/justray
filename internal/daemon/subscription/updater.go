package subscription

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/luynrs/justray/internal/daemon/store"
	"github.com/luynrs/justray/internal/shared/domain"
	"github.com/luynrs/justray/internal/shared/parser"
)

func (s *Service) RefreshAll(ctx context.Context, subs []store.Subscription, refresh func(context.Context, store.Subscription) (store.Subscription, error)) ([]store.Subscription, error) {
	errs := make([]error, len(subs))
	jobs := make(chan int)
	var wg sync.WaitGroup
	for range min(8, len(subs)) {
		wg.Go(func() {
			for i := range jobs {
				subs[i], errs[i] = refresh(ctx, subs[i])
			}
		})
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

	updated := make([]store.Subscription, 0, len(subs))
	for i, err := range errs {
		if err != nil {
			s.log.Print(err)
			continue
		}
		updated = append(updated, subs[i])
	}

	if failed := len(subs) - len(updated); failed > 0 {
		return updated, fmt.Errorf("subscription refresh failed (%d/%d)", failed, len(subs))
	}
	return updated, nil
}

func (s *Service) Refresh(ctx context.Context, sub store.Subscription) (store.Subscription, error) {
	if err := check(sub.URL); err != nil {
		return sub, err
	}
	if parser.IsLink(sub.URL) {
		n, err := parser.ParseURI(sub.URL)
		if err != nil {
			return sub, err
		}
		nodes := []domain.Node{n}
		if err := validateNodes(nodes); err != nil {
			return sub, err
		}
		sub.Nodes, sub.Name, sub.Traffic = nodes, n.Name, domain.Traffic{}
		sub.UpdatedAt = time.Now().UTC()
		return sub, nil
	}

	nodes, name, traffic, err := s.fetch(ctx, sub.URL)
	if err != nil {
		return sub, err
	}
	sub.Nodes, sub.Traffic, sub.UpdatedAt = nodes, traffic, time.Now().UTC()
	if name != "" { // change name if it changed on server
		sub.Name = name
	}
	return sub, nil
}

func validateNodes(nodes []domain.Node) error {
	for _, n := range nodes {
		if n.TLS != nil && n.TLS.Insecure {
			return fmt.Errorf("subscription contains an insecure node")
		}
	}
	return nil
}
