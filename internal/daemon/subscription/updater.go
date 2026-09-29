package subscription

import (
	"context"
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
		sub.Nodes, sub.Name, sub.Traffic = []domain.Node{n}, n.Name, domain.Traffic{}
		sub.UpdatedAt = time.Now().UTC()
		return sub, nil
	}

	nodes, name, traffic, warning, err := s.fetch(ctx, sub.URL)
	if err != nil {
		return sub, err
	}
	sub.Nodes, sub.Traffic, sub.UpdatedAt, sub.Warning = nodes, traffic, time.Now().UTC(), warning
	if name != "" { // change name if it changed on server
		sub.Name = name
	}
	return sub, nil
}
