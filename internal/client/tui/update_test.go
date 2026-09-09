package tui

import (
	"context"
	"errors"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/luynrs/justray/internal/client/tui/settings"
	"github.com/luynrs/justray/internal/domain"
	"github.com/luynrs/justray/internal/ipc"
)

func TestSettingsWaitForSnapshot(t *testing.T) {
	original, _ := (domain.Settings{}).Normalize()
	m := New(nil)
	defer m.stopWatch()
	m.snapshot.Settings = original
	m.dialog = settings.New(original, topLines)
	for range 3 {
		m.dialog.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	}
	m.dialog.Update(tea.KeyPressMsg{Code: tea.KeyRight})
	edited, changed, err := m.dialog.Result()
	if err != nil || !changed || edited.Emoji == original.Emoji {
		t.Fatalf("edit failed: changed=%v err=%v", changed, err)
	}
	m, _ = m.closeSettings()
	if !m.snapshot.Settings.Equal(original) {
		t.Fatal("closing the dialog applied unconfirmed settings")
	}
	updated, _ := m.Update(completed{op: "settings", err: errors.New("disk write failed")})
	m = updated.(Model)
	if !m.snapshot.Settings.Equal(original) || m.err == "" {
		t.Fatal("failed save changed confirmed settings or lost the error")
	}
	updated, _ = m.Update(pushed{live: true, snapshot: ipc.Snapshot{Settings: edited}})
	if !updated.(Model).snapshot.Settings.Equal(edited) {
		t.Fatal("daemon snapshot did not apply settings")
	}
}

func TestSnapshotAfterReconnect(t *testing.T) {
	m := New(nil)
	defer m.stopWatch()
	first := ipc.Snapshot{Revision: 100, Nodes: []ipc.Node{{ID: "old"}}}
	updated, _ := m.Update(pushed{live: true, snapshot: first})
	m = updated.(Model)
	updated, _ = m.Update(pushed{})
	m = updated.(Model)
	if m.live {
		t.Fatal("lost daemon is still live")
	}
	restarted := ipc.Snapshot{Revision: 1, Nodes: []ipc.Node{{ID: "new"}}}
	updated, _ = m.Update(pushed{live: true, snapshot: restarted})
	m = updated.(Model)
	if !m.live || m.snapshot.Revision != 1 || m.snapshot.Nodes[0].ID != "new" {
		t.Fatalf("new daemon's state was rejected: %+v", m.snapshot)
	}
}

func TestNextCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if msg := next(ctx, make(chan pushed))(); msg != nil {
		t.Fatalf("cancelled wait returned %v", msg)
	}
}

func TestSnapshotPreservesSelection(t *testing.T) {
	for _, cursor := range []int{0, 1} {
		m := New(nil)
		m.snapshot = ipc.Snapshot{
			Subscriptions: []ipc.Sub{{ID: "a"}, {ID: "b"}},
			Nodes:         []ipc.Node{{ID: "1", Sub: "a"}, {ID: "2", Sub: "b"}},
		}
		m.cursor = cursor
		selected, _ := m.at()
		reordered := m.snapshot
		reordered.Subscriptions = []ipc.Sub{{ID: "b"}, {ID: "a"}}
		updated, _ := m.Update(pushed{live: true, snapshot: reordered})
		row, ok := updated.(Model).at()
		m.stopWatch()
		if !ok || row.Kind != selected.Kind || row.Sub.ID != selected.Sub.ID || row.Node.Ref() != selected.Node.Ref() {
			t.Fatalf("cursor %d lost its selection: before=%+v after=%+v", cursor, selected, row)
		}
	}
}
