// Package subscription owns subscriptions.yaml
package subscription

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"slices"
	"sync"

	"github.com/luynrs/justray/internal/daemon/store"
	"github.com/luynrs/justray/internal/shared/parser"
	"github.com/luynrs/justray/internal/shared/rpc"
)

type Service struct {
	store  store.Disk
	device http.Header
	log    *log.Logger

	storeMu sync.Mutex
}

func New(st store.Disk, logger *log.Logger) *Service {
	device, err := deviceHeaders()
	if err != nil && logger != nil {
		logger.Print(err)
	}
	return &Service{store: st, device: device, log: logger}
}

func (s *Service) List() ([]rpc.Sub, error) {
	subs, err := s.store.Subscriptions()
	if err != nil {
		return nil, err
	}
	out := make([]rpc.Sub, len(subs))
	for i, sub := range subs {
		out[i] = info(sub)
	}
	return out, nil
}

func (s *Service) Add(ctx context.Context, rawURL string) (rpc.Sub, error) {
	if err := check(rawURL); err != nil {
		return rpc.Sub{}, err
	}

	sub := store.Subscription{ID: store.NewID(), URL: rawURL}
	if err := s.fill(ctx, &sub); err != nil {
		return rpc.Sub{}, err
	}
	if sub.Name == "" {
		sub.Name = host(rawURL)
	}

	s.storeMu.Lock()
	defer s.storeMu.Unlock()

	subs, err := s.store.Subscriptions()
	if err != nil {
		return rpc.Sub{}, err
	}
	return info(sub), s.store.Save(append(subs, sub))
}

// Remove deletes a subscription and returns it
func (s *Service) Remove(id string) (store.Subscription, error) {
	s.storeMu.Lock()
	defer s.storeMu.Unlock()

	subs, err := s.store.Subscriptions()
	if err != nil {
		return store.Subscription{}, err
	}
	i := slices.IndexFunc(subs, func(sub store.Subscription) bool { return sub.ID == id })
	if i < 0 {
		return store.Subscription{}, fmt.Errorf("subscription %q not found", id)
	}
	removed := subs[i]
	kept := slices.Delete(subs, i, i+1)
	return removed, s.store.Save(kept)
}

func (s *Service) MoveSub(id string, dir int) error {
	s.storeMu.Lock()
	defer s.storeMu.Unlock()

	subs, err := s.store.Subscriptions()
	if err != nil {
		return err
	}
	i := slices.IndexFunc(subs, func(sub store.Subscription) bool { return sub.ID == id })
	if i < 0 {
		return fmt.Errorf("subscription %q not found", id)
	}
	j := i + dir
	if j < 0 || j >= len(subs) {
		return nil
	}
	subs[i], subs[j] = subs[j], subs[i]
	return s.store.Save(subs)
}

func check(rawURL string) error {
	if rawURL == "" {
		return fmt.Errorf("paste a subscription url or a share link")
	}
	if parser.IsLink(rawURL) {
		return nil
	}
	u, err := url.Parse(rawURL)
	if err != nil || u.Host == "" || u.Scheme != "https" {
		if err == nil && u.Scheme == "http" {
			return fmt.Errorf("subscription must use https")
		}
		return fmt.Errorf("%q is not a url or a share link", rawURL)
	}
	return nil
}

func host(rawURL string) string {
	if u, err := url.Parse(rawURL); err == nil && u.Host != "" {
		return u.Host
	}
	return rawURL
}

func info(sub store.Subscription) rpc.Sub {
	return rpc.Sub{
		ID: sub.ID, Name: sub.Name,
		Nodes: len(sub.Nodes), UpdatedAt: sub.UpdatedAt,
		Traffic: sub.Traffic, Direct: parser.IsLink(sub.URL),
	}
}
