package subscription

import (
	"context"
	"log"
	"net/http"
	"net/url"

	"github.com/luynrs/justray/internal/daemon/store"
)

type Service struct {
	device http.Header
}

func New(ctx context.Context, logger *log.Logger) *Service {
	device, err := deviceHeaders(ctx)
	if err != nil && logger != nil {
		logger.Printf("device headers failed (%v)", err)
	}
	return &Service{device: device}
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
