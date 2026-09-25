package subscription

import (
	"context"
	"fmt"
	"time"

	"github.com/luynrs/justray/internal/daemon/store"
	"github.com/luynrs/justray/internal/domain"
	"github.com/luynrs/justray/internal/parser"
)

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
