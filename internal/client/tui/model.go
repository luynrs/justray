package tui

import (
	"context"
	"io"
	"log"
	"time"

	"charm.land/bubbles/v2/spinner"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"

	"github.com/luynrs/justray/internal/client/tui/settings"
	"github.com/luynrs/justray/internal/client/tui/style"
	"github.com/luynrs/justray/internal/client/tui/tree"
	"github.com/luynrs/justray/internal/ipc"
)

const (
	topLines    = 2 // title + gap
	footerLines = 3 // status + help
)

type Model struct {
	client *ipc.Client

	snapshot ipc.Snapshot

	collapsed map[string]bool
	spin      spinner.Model
	cursor    int
	scroll    int
	wheel     time.Time

	editor     textinput.Model
	confirmSub ipc.Sub
	dialog     *settings.Settings
	filter     textinput.Model

	live           bool
	updates        chan pushed
	watchCtx       context.Context
	stopWatch      context.CancelFunc
	connectionBusy bool

	err   string
	errAt time.Time

	w, h     int
	quitting bool
}

func New(c *ipc.Client) Model {
	watchCtx, stopWatch := context.WithCancel(context.Background())
	editor := textinput.New()
	editor.Prompt = "Add:  "
	editor.Placeholder = "subscription URL, or a vless://, vmess://, trojan://, ss://, etc. link"
	editor.CharLimit = 2048
	filter := textinput.New()
	filter.Prompt = ""
	filter.CharLimit = 128
	m := Model{
		client:    c,
		collapsed: map[string]bool{},
		spin:      spinner.New(),
		editor:    editor,
		filter:    filter,
		updates:   make(chan pushed),
		watchCtx:  watchCtx,
		stopWatch: stopWatch,
	}
	m.syncTTY()
	return m
}

func (m Model) Init() tea.Cmd {
	return tea.Batch(watch(m.watchCtx, m.client, m.updates), next(m.watchCtx, m.updates), tickCmd(), m.spin.Tick)
}

func (m *Model) syncTTY() {
	style.TTY = style.DetectTTY(m.forceTTY())
	m.spin.Spinner = spinner.MiniDot
	if style.TTY {
		m.spin.Spinner = spinner.Line
	}
}

func (m Model) forceTTY() string {
	if m.dialog != nil {
		return m.dialog.Current().ForceTTY
	}
	return m.snapshot.Settings.ForceTTY
}

func (m Model) emoji() bool {
	return !style.TTY && m.snapshot.Settings.Emoji == "on"
}

func (m Model) data() tree.Data {
	return tree.Data{
		Subs:      m.snapshot.Subscriptions,
		Nodes:     m.snapshot.Nodes,
		Collapsed: m.collapsed,
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
	return m, tea.Quit
}

func Run(c *ipc.Client) error {
	prev := log.Writer()
	defer log.SetOutput(prev)

	if dir, err := ipc.Dir(); err == nil {
		if f, err := tea.LogToFile(ipc.TUILog(dir), "tui"); err == nil {
			defer func() { _ = f.Close() }()
		} else {
			log.SetOutput(io.Discard)
		}
	} else {
		log.SetOutput(io.Discard)
	}

	m := New(c)
	defer m.stopWatch()
	_, err := tea.NewProgram(m).Run()
	return err
}
