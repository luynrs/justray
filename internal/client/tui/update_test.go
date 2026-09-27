package tui

import (
	"errors"
	"reflect"
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/luynrs/justray/internal/client/tui/settings"
	"github.com/luynrs/justray/internal/domain"
	"github.com/luynrs/justray/internal/ipc"
)

func TestSettingsWaitForSnapshot(t *testing.T) {
	original, _ := (domain.Settings{}).Normalize()
	model := New(nil, nil)
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
	updated, _ := model.Update(completed{op: "settings", err: errors.New("disk write failed")})
	model = updated.(Model)
	if !model.snapshot.Settings.Equal(original) || model.err == "" {
		t.Fatal("failed save changed confirmed settings or lost the error")
	}
	updated, _ = model.Update(pushed{live: true, snapshot: ipc.Snapshot{Settings: edited}})
	if !updated.(Model).snapshot.Settings.Equal(edited) {
		t.Fatal("daemon snapshot did not apply settings")
	}
}

func TestSnapshotAfterReconnect(t *testing.T) {
	model := New(nil, nil)
	defer model.stop()
	first := ipc.Snapshot{
		Nodes:         []ipc.Node{{ID: "old"}},
		Subscriptions: []ipc.Sub{{ID: "old-sub"}},
		Selected:      domain.NodeRef{NodeID: "old"},
		Status:        ipc.Status{Connected: true},
	}
	updated, _ := model.Update(pushed{live: true, snapshot: first})
	model = updated.(Model)
	updated, _ = model.Update(pushed{})
	model = updated.(Model)
	model.w = 80
	if model.live || !strings.Contains(model.footer(), "disconnected") {
		t.Fatal("lost daemon is still live or not disconnected")
	}
	restarted := ipc.Snapshot{Nodes: []ipc.Node{{ID: "new"}}}
	updated, _ = model.Update(pushed{live: true, snapshot: restarted})
	model = updated.(Model)
	if !model.live || !reflect.DeepEqual(model.snapshot, restarted) {
		t.Fatalf("new daemon's state was rejected: %+v", model.snapshot)
	}
}

func TestCollapseFollowsDaemonSnapshot(t *testing.T) {
	model := New(nil, nil)
	defer model.stop()
	model.w, model.h = 80, 24
	snapshot := ipc.Snapshot{
		Settings:      domain.Settings{Port: 10808},
		Subscriptions: []ipc.Sub{{ID: "sub", Name: "Subscription"}},
		Nodes:         []ipc.Node{{ID: "node", Sub: "sub", Name: "visible-node"}},
	}
	updated, _ := model.Update(pushed{live: true, snapshot: snapshot})
	model = updated.(Model)
	if !strings.Contains(model.tree(), "visible-node") {
		t.Fatal("node hidden before collapse")
	}
	updated, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	model = updated.(Model)
	if !strings.Contains(model.tree(), "visible-node") {
		t.Fatal("collapse applied before daemon confirmation")
	}
	snapshot.Collapsed = []string{"sub"}
	updated, _ = model.Update(pushed{live: true, snapshot: snapshot})
	model = updated.(Model)
	if strings.Contains(model.tree(), "visible-node") {
		t.Fatal("daemon collapse was not applied")
	}
	snapshot.Collapsed = nil
	updated, _ = model.Update(pushed{live: true, snapshot: snapshot})
	model = updated.(Model)
	if !strings.Contains(model.tree(), "visible-node") {
		t.Fatal("daemon expansion was not applied")
	}
	snapshot.Subscriptions = append(snapshot.Subscriptions, ipc.Sub{ID: "other", Name: "Other"})
	snapshot.Nodes = append(snapshot.Nodes, ipc.Node{ID: "other-node", Sub: "other", Name: "other-node"})
	updated, _ = model.Update(pushed{live: true, snapshot: snapshot})
	model = updated.(Model)
	updated, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	model = updated.(Model)
	snapshot.Collapsed = []string{"sub"}
	updated, _ = model.Update(pushed{live: true, snapshot: snapshot})
	if row, _ := updated.(Model).at(); row.Sub.ID != "sub" {
		t.Fatal("remote collapse moved selection to another subscription")
	}
}

func TestRoutingRuleThroughSettings(t *testing.T) {
	model := New(nil, nil)
	defer model.stop()
	model.snapshot.Settings, _ = (domain.Settings{}).Normalize()
	for _, key := range []tea.KeyPressMsg{{Code: 'o'}, {Code: tea.KeyTab}, {Code: tea.KeyTab},
		{Code: tea.KeyDown}, {Code: tea.KeyDown}, {Code: tea.KeyDown}, {Code: tea.KeyDown}, {Code: tea.KeyEnter}} {
		updated, _ := model.Update(key)
		model = updated.(Model)
	}
	updated, _ := model.Update(tea.PasteMsg{Content: "example.com"})
	model = updated.(Model)
	updated, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	model = updated.(Model)
	if !slices.Equal(model.dialog.Current().Direct, []string{"example.com"}) {
		t.Fatalf("rule was not added: %v", model.dialog.Current().Direct)
	}
	updated, _ = model.Update(tea.KeyPressMsg{Code: 'd'})
	if len(updated.(Model).dialog.Current().Direct) != 0 {
		t.Fatal("rule was not removed")
	}
}
