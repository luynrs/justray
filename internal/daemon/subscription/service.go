package subscription

import (
	"context"
	"net/http"
	"net/url"

	"github.com/sagernet/sing-box/log"

	"github.com/luynrs/justray/internal/daemon/store"
)

type Service struct {
	device http.Header
	slots  chan struct{}
}

func New(ctx context.Context) *Service {
	device, err := deviceHeaders(ctx)
	if err != nil {
		log.Warn("cannot read device headers: ", err)
	}
	return &Service{device: device, slots: make(chan struct{}, 8)}
}

func (s *Service) PrepareAdd(ctx context.Context, rawURL string) (store.Subscription, error) {
	sub, err := s.Refresh(ctx, store.Subscription{ID: store.NewID(), URL: rawURL})
	if err != nil {
		return store.Subscription{}, err
	}
	if sub.Name == "" {
		parsedURL, _ := url.Parse(rawURL)
		sub.Name = parsedURL.Host
	}
	return sub, nil
}
