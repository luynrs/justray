package server

import "time"

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
		next := map[string]time.Time{}
		var ids []string
		for _, sub := range snapshot.Subscriptions {
			if !sub.Refreshable || time.Since(sub.UpdatedAt) < every {
				continue
			}
			lastAttempt := tried[sub.SubscriptionID]
			if time.Since(lastAttempt) >= 15*time.Minute {
				lastAttempt = time.Now()
				ids = append(ids, sub.SubscriptionID)
			}
			next[sub.SubscriptionID] = lastAttempt
		}
		tried = next
		if len(ids) > 0 {
			_ = s.core.RefreshSubscriptions(s.ctx, ids...)
		}
	}
}
