package tui

import (
	"errors"
	"reflect"
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
