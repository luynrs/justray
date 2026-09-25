package server

import (
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
		var ids []string
		for id := range active {
			if time.Since(tried[id]) < 15*time.Minute {
				continue
			}
			tried[id] = time.Now()
			ids = append(ids, id)
		}
		if len(ids) > 0 {
			if err := s.core.RefreshSubscriptions(s.ctx, ids...); err != nil {
				s.log.Printf("auto-refresh failed (%v)", err)
			}
		}
	}
}
