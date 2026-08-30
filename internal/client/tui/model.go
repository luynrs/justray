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
	"github.com/luynrs/justray/internal/client/tui/tree"
	"github.com/luynrs/justray/internal/shared/domain"
	"github.com/luynrs/justray/internal/shared/rpc"
)

const (
	topLines    = 2 // title + gap
	footerLines = 3 // status + help
)

type Model struct {
	client *rpc.Client

	subs  []rpc.Sub
	nodes []rpc.Node

	collapsed  map[string]bool
	probing    map[domain.NodeRef]bool
	refreshing map[string]bool
	spin       spinner.Model
	cursor     int
	scroll     int
	wheel      time.Time

	editor    textinput.Model
	editing   bool
	confirmQ  string
	confirmID string
	settings  *settings.Settings
	filtering bool
	filter    textinput.Model
	query     string

	status     rpc.Status
	revision   uint64
	live       bool
	emoji      bool
	since      time.Time
	statusCh   chan pushed
	watchCtx   context.Context
	stopWatch  context.CancelFunc
	connecting bool

	err string

	w, h     int
	quitting bool
}

func New(c *rpc.Client) Model {
	watchCtx, stopWatch := context.WithCancel(context.Background())
	editor := textinput.New()
	editor.Prompt = "Add:  "
	editor.Placeholder = "subscription URL, or a vless://, vmess://, trojan://, ss://, etc. link"
	editor.CharLimit = 2048
	filter := textinput.New()
	filter.Placeholder, filter.CharLimit = "type to filter...", 128
	return Model{
		client:    c,
		collapsed: map[string]bool{},
		spin:      spinner.New(spinner.WithSpinner(spinner.MiniDot)),
		editor:    editor,
		filter:    filter,
		statusCh:  make(chan pushed),
		watchCtx:  watchCtx,
		stopWatch: stopWatch,
	}
}

func (m Model) Init() tea.Cmd {
	return tea.Batch(func() tea.Msg { return load(m.client) }, watch(m.watchCtx, m.client, m.statusCh), next(m.statusCh), tickCmd(), m.spin.Tick)
}

func (m Model) data() tree.Data {
	return tree.Data{
		Subs:       m.subs,
		Nodes:      m.nodes,
		Collapsed:  m.collapsed,
		Probing:    m.probing,
		Refreshing: m.refreshing,
		Query:      m.query,
		Status:     m.status,
		Live:       m.live,
		Emoji:      m.emoji,
		Spinner:    m.spin.View(),
	}
}

func (m Model) rows() []tree.Row { return m.data().Rows() }

func (m Model) at() (tree.Row, bool) { return tree.At(m.rows(), m.cursor) }

func (m Model) connected() bool { return m.live && m.status.Connected }

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

func Run(c *rpc.Client) error {
	log.SetOutput(io.Discard)
	m := New(c)
	_, err := tea.NewProgram(m).Run()
	m.stopWatch()
	return err
}
