package subscription

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/luynrs/justray/internal/daemon/store"
)

func TestRefreshPreservesDialErrorChain(t *testing.T) {
	s := &Service{device: map[string][]string{"X-Hwid": {"test-device"}}}
	sub := store.Subscription{URL: "http://127.0.0.1:1/sub"} // nothing listening
	_, err := s.Refresh(context.Background(), sub)
	if err == nil {
		t.Fatal("expected dial error")
	}
	msg := err.Error()
	if !strings.HasPrefix(msg, "fetch subscription: ") {
		t.Fatalf("want wrapped prefix, got %q", msg)
	}
	if strings.Contains(msg, "%!w(<nil>)") {
		t.Fatalf("Unwrap dropped the cause: %q", msg)
	}
	if !strings.Contains(msg, "127.0.0.1:1") && !errors.Is(err, err) {
		// URL should still be present in the http client's wrapped error
		t.Logf("error message: %q", msg)
	}
	// The important invariant: wrapping with the full err keeps a non-nil cause.
	if errors.Unwrap(err) == nil {
		t.Fatalf("expected non-nil unwrapped cause, got %q", msg)
	}
}
