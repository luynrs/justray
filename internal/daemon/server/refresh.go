package server

import (
	"sync"
	"time"
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
		active := map[string]struct{}{}
		for _, sub := range snapshot.Subscriptions {
			if !sub.Direct && time.Since(sub.UpdatedAt) >= every {
				active[sub.ID] = struct{}{}
			}
		}
		for id := range tried {
			if _, ok := active[id]; !ok {
				delete(tried, id)
			}
		}
		var wg sync.WaitGroup
		sem := make(chan struct{}, 4)
		for id := range active {
			if time.Since(tried[id]) < 15*time.Minute {
				continue
			}
			tried[id] = time.Now()
			wg.Go(func() {
				select {
				case sem <- struct{}{}:
					defer func() { <-sem }()
				case <-s.ctx.Done():
					return
				}
				if err := s.core.RefreshSubscription(s.ctx, id); err != nil {
					s.log.Print(err)
				}
			})
		}
		wg.Wait()
	}
}
