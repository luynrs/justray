package server

import (
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
		for _, id := range stale {
			if time.Since(tried[id]) < 15*time.Minute {
				continue
			}
			tried[id] = time.Now()
			if _, err := s.core.RefreshSubscription(s.ctx, id); err != nil {
				s.log.Print(err)
			}
		}
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
