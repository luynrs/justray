package tui

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/luynrs/justray/internal/client/tui/settings"
	"github.com/luynrs/justray/internal/domain"
	"github.com/luynrs/justray/internal/ipc"
)

func TestActionStartFailure(t *testing.T) {
	model := New(ipc.New("missing-daemon.sock"), func(context.Context) error {
		return errors.New("launcher failed")
	}, nil)
	defer model.stop()
	updated, command := model.Update(tea.KeyPressMsg{Code: 'm'})
	updated, _ = updated.Update(command())
	model = updated.(Model)
	model.w, model.h = 80, 24
	if !strings.Contains(model.View().Content, "launcher failed") {
		t.Fatalf("launcher error missing from footer: %s", model.View().Content)
	}
}

func TestSettingsSnapshot(t *testing.T) {
	original, _ := (domain.Settings{}).Normalize()
	model := New(nil, nil, nil)
	defer model.stop()
	model.snapshot.Settings = original
	model.dialog = settings.New(original, topLines)
	for range 2 {
		model.dialog.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	}
	model.dialog.Update(tea.KeyPressMsg{Code: tea.KeyRight})
	edited, changed, err := model.dialog.Result()
	if err != nil || !changed || edited.Emoji == original.Emoji {
		t.Fatalf("edit failed: changed=%v err=%v", changed, err)
	}
	model, _ = model.closeSettings()
	if !model.snapshot.Settings.Equal(original) {
		t.Fatal("closing the dialog applied unconfirmed settings")
	}
	updated, _ := model.Update(completed{err: errors.New("disk write failed")})
	model = updated.(Model)
	if !model.snapshot.Settings.Equal(original) || model.err == "" {
		t.Fatal("failed save changed confirmed settings or lost the error")
	}
	updated, _ = model.Update(pushed{live: true, snapshot: ipc.Snapshot{Settings: edited}})
	if !updated.(Model).snapshot.Settings.Equal(edited) {
		t.Fatal("daemon snapshot did not apply settings")
	}
}

func TestReconnectSnapshot(t *testing.T) {
	model := New(nil, nil, nil)
	defer model.stop()
	first := ipc.Snapshot{
		Nodes:         []ipc.Node{{NodeID: "old"}},
		Subscriptions: []ipc.Subscription{{SubscriptionID: "old-sub"}},
		Selected:      domain.NodeRef{NodeID: "old"},
		Status:        ipc.Status{Connected: true},
	}
	updated, _ := model.Update(pushed{live: true, snapshot: first})
	model = updated.(Model)
	updated, _ = model.Update(pushed{})
	model = updated.(Model)
	model.w = 80
	if model.live || !strings.Contains(model.footer(model.rows()), "disconnected") {
		t.Fatal("lost daemon is still live or not disconnected")
	}
	restarted := ipc.Snapshot{Nodes: []ipc.Node{{NodeID: "new"}}}
	updated, _ = model.Update(pushed{live: true, snapshot: restarted})
	model = updated.(Model)
	if !model.live || !reflect.DeepEqual(model.snapshot, restarted) {
		t.Fatalf("new daemon's state was rejected: %+v", model.snapshot)
	}
}

func TestCollapseSnapshot(t *testing.T) {
	model := New(nil, nil, nil)
	defer model.stop()
	model.w, model.h = 80, 24
	snapshot := ipc.Snapshot{
		Settings:      domain.Settings{Connection: domain.Connection{Port: 10808}},
		Subscriptions: []ipc.Subscription{{SubscriptionID: "sub", Name: "Subscription"}},
		Nodes:         []ipc.Node{{NodeID: "node", SubscriptionID: "sub", Name: "visible-node"}},
	}
	updated, _ := model.Update(pushed{live: true, snapshot: snapshot})
	model = updated.(Model)
	if !strings.Contains(model.tree(model.rows()), "visible-node") {
		t.Fatal("node hidden before collapse")
	}
	updated, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	model = updated.(Model)
	if !strings.Contains(model.tree(model.rows()), "visible-node") {
		t.Fatal("collapse applied before daemon confirmation")
	}
	snapshot.Collapsed = []string{"sub"}
	updated, _ = model.Update(pushed{live: true, snapshot: snapshot})
	model = updated.(Model)
	if strings.Contains(model.tree(model.rows()), "visible-node") {
		t.Fatal("daemon collapse was not applied")
	}
	snapshot.Collapsed = nil
	updated, _ = model.Update(pushed{live: true, snapshot: snapshot})
	model = updated.(Model)
	if !strings.Contains(model.tree(model.rows()), "visible-node") {
		t.Fatal("daemon expansion was not applied")
	}
	snapshot.Subscriptions = append(snapshot.Subscriptions, ipc.Subscription{SubscriptionID: "other", Name: "Other"})
	snapshot.Nodes = append(snapshot.Nodes, ipc.Node{NodeID: "other-node", SubscriptionID: "other", Name: "other-node"})
	updated, _ = model.Update(pushed{live: true, snapshot: snapshot})
	model = updated.(Model)
	updated, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	model = updated.(Model)
	snapshot.Collapsed = []string{"sub"}
	updated, _ = model.Update(pushed{live: true, snapshot: snapshot})
	if row, _ := updated.(Model).at(); row.Sub.SubscriptionID != "sub" {
		t.Fatal("remote collapse moved selection to another subscription")
	}
}
