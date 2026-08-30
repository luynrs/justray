package server

import (
	"sync"
	"time"

	"github.com/luynrs/justray/internal/shared/rpc"
)

func (s *Server) AutoRefresh() {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	tried := map[string]time.Time{}
	for {
		select {
		case <-s.ctx.Done():
			return
		case <-ticker.C:
		}

		snapshot := s.core.Snapshot()
		every := time.Duration(snapshot.Settings.RefreshEvery) * time.Hour
		if every == 0 {
			continue
		}
		stale := stale(snapshot.Subscriptions, every)
		active := make(map[string]struct{}, len(stale))
		for _, id := range stale {
			active[id] = struct{}{}
		}
		for id := range tried {
			if _, ok := active[id]; !ok {
				delete(tried, id)
			}
		}
		var wg sync.WaitGroup
		sem := make(chan struct{}, 4)
		for _, id := range stale {
			if time.Since(tried[id]) < 15*time.Minute {
				continue
			}
			tried[id] = time.Now()
			subID := id
			wg.Add(1)
			go func() {
				defer wg.Done()
				select {
				case sem <- struct{}{}:
					defer func() { <-sem }()
				case <-s.ctx.Done():
					return
				}
				if _, err := s.core.RefreshSubscription(s.ctx, subID); err != nil {
					s.log.Print(err)
				}
			}()
		}
		wg.Wait()
	}
}

func stale(list []rpc.Sub, every time.Duration) []string {
	var out []string
	for _, sub := range list {
		if time.Since(sub.UpdatedAt) >= every {
			out = append(out, sub.ID)
		}
	}
	return out
}
