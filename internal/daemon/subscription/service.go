package subscription

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"net/url"

	"github.com/luynrs/justray/internal/daemon/store"
	"github.com/luynrs/justray/internal/shared/parser"
	"github.com/luynrs/justray/internal/shared/rpc"
)

type Service struct {
	device http.Header
	log    *log.Logger
}

func New(ctx context.Context, logger *log.Logger) *Service {
	device, err := deviceHeaders(ctx)
	if err != nil && logger != nil {
		logger.Print(err)
	}
	return &Service{device: device, log: logger}
}

func (s *Service) PrepareAdd(ctx context.Context, rawURL string) (store.Subscription, error) {
	if err := check(rawURL); err != nil {
		return store.Subscription{}, err
	}

	sub := store.Subscription{ID: store.NewID(), URL: rawURL}
	if err := s.fill(ctx, &sub); err != nil {
		return store.Subscription{}, err
	}
	if sub.Name == "" {
		sub.Name = host(rawURL)
	}
	return sub, nil
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

func Info(sub store.Subscription) rpc.Sub {
	return rpc.Sub{
		ID: sub.ID, Name: sub.Name,
		Nodes: len(sub.Nodes), UpdatedAt: sub.UpdatedAt,
		Traffic: sub.Traffic, Direct: parser.IsLink(sub.URL),
	}
}
