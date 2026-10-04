package tui

import (
	"context"
	"io"
	"log"
	"os"
	"slices"
	"time"

	"charm.land/bubbles/v2/spinner"
	"charm.land/bubbles/v2/textarea"
	"charm.land/bubbles/v2/textinput"
	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/luynrs/justray/internal/client/tui/settings"
	"github.com/luynrs/justray/internal/client/tui/style"
	"github.com/luynrs/justray/internal/client/tui/tree"
	"github.com/luynrs/justray/internal/ipc"
)

const (
	topLines    = 2 // title + gap
	footerLines = 2 // status + help
)

type Model struct {
	client *ipc.Client

	snapshot ipc.Snapshot

	spin          spinner.Model
	spinnerActive bool
	cursor        int
	scroll        int
	wheel         time.Time

	editor       textarea.Model
	deleteTarget tree.Row
	dialog       *settings.Model
	filter       textinput.Model
	help         viewport.Model
	activeModal  modalKind
	helpReturn   modalKind

	live    bool
	updates chan pushed
	watch   context.Context
	stop    context.CancelFunc
	busy    bool
	start   func(manual bool) func() error
	restore tea.Cmd

	err   string
	errAt time.Time

	w, h     int
	quitting bool
}

func New(c *ipc.Client, start, restore func(context.Context) error) Model {
	watch, stop := context.WithCancel(context.Background())
	ctx, cancel := context.WithCancel(watch)
	editor := textarea.New()
	editor.Prompt = ""
	editor.ShowLineNumbers = false
	editor.Placeholder = "Paste a subscription or node link..."
	editor.CharLimit = 2048
	editor.KeyMap.InsertNewline.SetEnabled(false)
	editorStyles := editor.Styles()
	editorStyles.Focused.CursorLine = lipgloss.NewStyle()
	editorStyles.Focused.Placeholder = style.Dim
	editor.SetStyles(editorStyles)
	filter := textinput.New()
	filter.Prompt = ""
	filter.CharLimit = 128
	m := Model{
		client:        c,
		spin:          spinner.New(),
		spinnerActive: true,
		editor:        editor,
		filter:        filter,
		help:          viewport.New(),
		updates:       make(chan pushed),
		watch:         watch,
		stop:          stop,
		start: func(manual bool) func() error {
			if manual {
				cancel()
			}
			if start == nil {
				return nil
			}
			return func() error { return start(watch) }
		},
	}
	m.help.SoftWrap = true
	if restore != nil {
		m.restore = func() tea.Msg {
			defer cancel()
			return restored{err: restore(ctx)}
		}
	} else {
		cancel()
	}
	m.syncTTY()
	return m
}

func (m Model) Init() tea.Cmd {
	return tea.Batch(watch(m.watch, m.client, m.updates), next(m.watch, m.updates), tickCmd(), m.spin.Tick, m.restore)
}

func (m Model) needsSpinner() bool {
	return m.busy || m.restore != nil ||
		slices.ContainsFunc(m.snapshot.Nodes, func(node ipc.Node) bool { return node.Probing }) ||
		slices.ContainsFunc(m.snapshot.Subscriptions, func(subscription ipc.Subscription) bool { return subscription.Refreshing })
}

func (m *Model) startSpinner() tea.Cmd {
	if m.spinnerActive || !m.needsSpinner() {
		return nil
	}
	m.spinnerActive = true
	return m.spin.Tick
}

func (m *Model) syncTTY() {
	force := m.snapshot.Settings.ForceTTY
	if m.dialog != nil {
		force = m.dialog.Current().ForceTTY
	}
	style.TTY = style.DetectTTY(force)
	m.spin.Spinner = spinner.MiniDot
	if style.TTY {
		m.spin.Spinner = spinner.Line
	}
}

func (m Model) emoji() bool {
	return !style.TTY && m.snapshot.Settings.Emoji == "on"
}

func (m Model) editing() bool {
	return m.filter.Focused() || m.dialog != nil && m.dialog.Editing()
}

func (m Model) data() tree.Data {
	return tree.Data{
		Subs:      m.snapshot.Subscriptions,
		Nodes:     m.snapshot.Nodes,
		Collapsed: m.snapshot.Collapsed,
		Query:     m.filter.Value(),
		Status:    m.snapshot.Status,
		Live:      m.live,
		Emoji:     m.emoji(),
		Spinner:   m.spin.View(),
	}
}

func (m Model) rows() []tree.Row { return m.data().Rows() }

func (m Model) at() (tree.Row, bool) { return tree.At(m.rows(), m.cursor) }

func (m Model) connected() bool { return m.live && m.snapshot.Status.Connected }

func (m Model) height() int { return max(m.h-topLines-footerLines, 1) }

func (m *Model) move(delta int) {
	m.cursor += delta
	m.clamp()
}

func (m *Model) clamp() {
	m.cursor, m.scroll = tree.Clamp(m.rows(), m.cursor, m.scroll, m.height())
}

func (m Model) quit() (tea.Model, tea.Cmd) {
	m.quitting = true
	m.stop()
	return m, tea.Quit
}

func Run(c *ipc.Client, start, restore func(context.Context) error) error {
	defer log.SetOutput(log.Writer())
	defer log.SetPrefix(log.Prefix())
	defer log.SetFlags(log.Flags())
	log.SetOutput(io.Discard)
	log.SetPrefix("tui: ")
	log.SetFlags(log.LstdFlags)

	if dir, err := ipc.Dir(); err == nil {
		if f, err := os.OpenFile(ipc.TUILog(dir), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600); err == nil {
			defer func() { _ = f.Close() }()
			log.SetOutput(f)
		}
	}

	m := New(c, start, restore)
	defer m.stop()
	_, err := tea.NewProgram(m).Run()
	return err
}
