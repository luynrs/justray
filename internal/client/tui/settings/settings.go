package settings

import (
	"cmp"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"

	"github.com/luynrs/justray/internal/client/tui/navigation"
	"github.com/luynrs/justray/internal/client/tui/style"
	"github.com/luynrs/justray/internal/domain"
)

type field struct {
	name  string
	bare  bool   // the row is its own value, like a routing rule
	hint  string // the editor placeholder, and the value shown while unset
	get   func(domain.Settings) string
	set   func(*domain.Settings, string) error
	enum  []string
	list  *list
	index int
}

type tab struct {
	name   string
	lists  []list
	fields []field
}

// list is an editable group of rules under a heading
type list struct {
	title string
	at    func(*domain.Settings) *[]string
}

var tabs = []tab{
	{name: "General", fields: []field{
		{
			name: "Autostart",
			enum: domain.Toggle,
			get:  func(s domain.Settings) string { return cmp.Or(s.Autostart, "unknown") },
			set:  func(s *domain.Settings, in string) error { s.Autostart = in; return nil },
		},
		{
			name: "Auto-refresh",
			hint: "never",
			get:  func(s domain.Settings) string { return hours(s.RefreshEvery) },
			set: func(s *domain.Settings, in string) error {
				v, err := parseHours(in)
				if err != nil {
					return err
				}
				s.RefreshEvery = v
				return nil
			},
		},
		{
			name: "Emoji",
			enum: domain.Toggle,
			get:  func(s domain.Settings) string { return s.Emoji },
			set:  func(s *domain.Settings, in string) error { s.Emoji = in; return nil },
		},
		{
			name: "Force TTY",
			enum: domain.Toggle,
			get:  func(s domain.Settings) string { return s.ForceTTY },
			set:  func(s *domain.Settings, in string) error { s.ForceTTY = in; return nil },
		},
		{
			name: "Log level",
			enum: domain.LogLevels,
			get:  func(s domain.Settings) string { return s.LogLevel },
			set:  func(s *domain.Settings, in string) error { s.LogLevel = in; return nil },
		},
		{
			name: "Probe URL",
			hint: domain.DefaultProbeURL,
			get:  func(s domain.Settings) string { return s.ProbeURL },
			set:  setStr(func(s *domain.Settings) *string { return &s.ProbeURL }, domain.DefaultProbeURL),
		},
	}},
	{name: "Connection", fields: []field{
		{
			name: "Proxy port",
			hint: strconv.Itoa(domain.DefaultPort),
			get:  func(s domain.Settings) string { return strconv.Itoa(s.Port) },
			set:  setInt(func(s *domain.Settings) *int { return &s.Port }, domain.DefaultPort),
		},
		{
			name: "Allow LAN",
			enum: domain.Toggle,
			get:  func(s domain.Settings) string { return s.AllowLAN },
			set:  func(s *domain.Settings, in string) error { s.AllowLAN = in; return nil },
		},
		{
			name: "IP version",
			enum: domain.IPVersions,
			get:  func(s domain.Settings) string { return s.IPVersion },
			set:  func(s *domain.Settings, in string) error { s.IPVersion = in; return nil },
		},
		{
			name: "Stack",
			enum: domain.TunStacks,
			get:  func(s domain.Settings) string { return s.TunStack },
			set:  func(s *domain.Settings, in string) error { s.TunStack = in; return nil },
		},
		{
			name: "MTU",
			hint: strconv.Itoa(domain.DefaultTunMTU),
			get:  func(s domain.Settings) string { return strconv.Itoa(s.TunMTU) },
			set:  setInt(func(s *domain.Settings) *int { return &s.TunMTU }, domain.DefaultTunMTU),
		},
		{
			name: "DNS hijack",
			enum: domain.Toggle,
			get:  func(s domain.Settings) string { return s.DNSHijack },
			set:  func(s *domain.Settings, in string) error { s.DNSHijack = in; return nil },
		},
		{
			name: "DNS server",
			hint: domain.DefaultDNS,
			get:  func(s domain.Settings) string { return s.DNS },
			set:  setStr(func(s *domain.Settings) *string { return &s.DNS }, domain.DefaultDNS),
		},
	}},
	{name: "Routing", fields: []field{
		{
			name: "Mode",
			enum: domain.Modes,
			get:  func(s domain.Settings) string { return s.Mode },
			set:  func(s *domain.Settings, in string) error { s.Mode = in; return nil },
		},
		{
			name: "Direct LAN",
			enum: domain.Toggle,
			get:  func(s domain.Settings) string { return s.BypassLocal },
			set:  func(s *domain.Settings, in string) error { s.BypassLocal = in; return nil },
		},
		{
			name: "Strict route",
			enum: domain.Toggle,
			get:  func(s domain.Settings) string { return s.TunStrict },
			set:  func(s *domain.Settings, in string) error { s.TunStrict = in; return nil },
		},
		{
			name: "Block QUIC",
			enum: domain.Toggle,
			get:  func(s domain.Settings) string { return s.BlockQUIC },
			set:  func(s *domain.Settings, in string) error { s.BlockQUIC = in; return nil },
		},
	}, lists: []list{
		{"Direct", func(v *domain.Settings) *[]string { return &v.Direct }},
		{"Proxy", func(v *domain.Settings) *[]string { return &v.Proxy }},
		{"Block", func(v *domain.Settings) *[]string { return &v.Block }},
	}},
}

func setStr(at func(*domain.Settings) *string, def string) func(*domain.Settings, string) error {
	return func(s *domain.Settings, in string) error {
		*at(s) = cmp.Or(strings.TrimSpace(in), def)
		return nil
	}
}

func setInt(at func(*domain.Settings) *int, def int) func(*domain.Settings, string) error {
	return func(s *domain.Settings, in string) error {
		if in = strings.TrimSpace(in); in == "" {
			*at(s) = def
			return nil
		}
		v, err := strconv.Atoi(in)
		if err != nil {
			return fmt.Errorf("%q is not a number", in)
		}
		*at(s) = v
		return nil
	}
}

type Model struct {
	top         int
	hits        map[int]hit
	tab         int
	cursor      int
	scroll      int
	height      int
	navigation  navigation.Keys
	abandon     bool
	input       textinput.Model
	cur         domain.Settings
	orig        domain.Settings
	err         string
	compactTabs bool
}

func New(s domain.Settings, top int) *Model {
	input := textinput.New()
	input.Prompt = ""
	input.SetStyles(style.Input)
	input.CharLimit = 2048
	m := &Model{top: top, height: 1, cur: s.Clone(), orig: s, input: input}
	m.move(0)
	return m
}

func (s *Model) Result() (domain.Settings, domain.Settings, error) {
	if s.abandon || !s.Dirty() {
		return s.orig, s.orig, nil
	}
	next, err := s.cur.Normalize()
	return next, s.orig, err
}

func (s *Model) Current() domain.Settings { return s.cur }

func (s *Model) Err() string { return s.err }

func (s *Model) Editing() bool { return s.input.Focused() }

func (s *Model) CancelNavigation() { s.navigation.Reset() }

func (s *Model) Update(msg tea.Msg) (closed bool, cmd tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		return s.key(msg)
	case tea.MouseMsg:
		s.navigation.Reset()
		return false, s.mouse(msg)
	}
	if s.input.Focused() {
		var cmd tea.Cmd
		s.input, cmd = s.input.Update(msg)
		return false, cmd
	}
	return false, nil
}

func (s *Model) key(msg tea.KeyPressMsg) (bool, tea.Cmd) {
	if s.input.Focused() {
		return false, s.editKey(msg)
	}
	if motion, handled := s.navigation.Read(msg.String()); handled {
		s.navigate(motion)
		return false, nil
	}

	switch msg.String() {
	case "esc":
		if s.err != "" {
			s.abandon = true
			return true, nil
		}
		if _, _, err := s.Result(); err != nil {
			s.err = err.Error()
			return false, nil
		}
		return true, nil

	case "q":
		s.abandon = true
		return true, nil

	case "right", "l":
		s.step(1)
	case "left", "h":
		s.step(-1)
	case "tab":
		s.switchTab(1)
	case "shift+tab":
		s.switchTab(-1)

	case "enter":
		return false, s.activate()

	case "d":
		if f, ok := s.at(); ok && f.removable() {
			_ = f.apply(&s.cur, "")
			s.move(0)
		}
	}
	return false, nil
}

func (s *Model) mouse(msg tea.MouseMsg) tea.Cmd {
	if s.input.Focused() {
		return nil
	}
	mouse := msg.Mouse()
	y := mouse.Y - s.top

	switch msg.(type) {
	case tea.MouseClickMsg:
		if mouse.Button != tea.MouseLeft {
			return nil
		}
		if mouse.Y == 0 {
			if i, ok := s.tabAt(mouse.X); ok {
				s.switchTab(i - s.tab)
			}
			return nil
		}
		if y < 0 {
			return nil
		}
		h, ok := s.hits[y]
		if !ok {
			return nil
		}
		if h.choice != "" {
			if f, ok := s.at(); ok {
				s.assign(f, h.choice)
			}
			return nil
		}
		if rows := s.rows(); h.row < len(rows) && !rows[h.row].editable() {
			return nil
		}
		if h.row == s.cursor {
			return s.activate()
		}
		s.cursor = h.row
		s.move(0)

	case tea.MouseWheelMsg:
		if y < 0 || y >= s.height {
			return nil
		}
		switch mouse.Button {
		case tea.MouseWheelUp:
			s.scrollBy(-3)
		case tea.MouseWheelDown:
			s.scrollBy(3)
		}
	}
	return nil
}

func (s *Model) activate() tea.Cmd {
	s.move(0)
	f, ok := s.at()
	if !ok || !f.editable() {
		return nil
	}
	if len(f.enum) > 0 {
		s.step(1)
		return nil
	}

	s.err = ""
	s.input.Placeholder = f.hint
	s.input.SetValue(f.value(s.cur))
	s.input.CursorEnd()
	return s.input.Focus()
}

func (s *Model) editKey(msg tea.KeyPressMsg) tea.Cmd {
	switch msg.String() {
	case "esc":
		s.err = ""
		s.input.Blur()
		return nil

	case "enter":
		f, ok := s.at()
		if !ok {
			s.input.Blur()
			return nil
		}
		if err := f.apply(&s.cur, s.input.Value()); err != nil {
			s.err = err.Error()
			return nil
		}
		s.err = ""
		s.input.Blur()
		s.move(0)
		return nil
	}

	var cmd tea.Cmd
	s.input, cmd = s.input.Update(msg)
	return cmd
}

// step cycles an enum choice
func (s *Model) step(delta int) {
	f, ok := s.at()
	if !ok || len(f.enum) == 0 {
		return
	}
	i := (slices.Index(f.enum, f.value(s.cur)) + delta + len(f.enum)) % len(f.enum)
	s.assign(f, f.enum[i])
}

func (s *Model) assign(f field, v string) {
	s.err = ""
	if !f.editable() {
		return
	}
	if err := f.apply(&s.cur, v); err != nil {
		s.err = err.Error()
	}
}

func (s *Model) rows() []field {
	t := &tabs[s.tab]
	if len(t.lists) == 0 {
		return t.fields
	}
	out := slices.Clone(t.fields)
	for i := range t.lists {
		out = append(out, s.listRows(&t.lists[i])...)
	}
	return out
}

// listRows is a heading, its entries and an add row
func (s *Model) listRows(l *list) []field {
	entries := *l.at(&s.cur)
	out := make([]field, 0, len(entries)+2)
	out = append(out, field{name: l.title, hint: fmt.Sprintf("(%d)", len(entries))})
	for i, entry := range entries {
		out = append(out, field{name: entry, bare: true, hint: "delete?", list: l, index: i})
	}
	return append(out, field{name: "+ add rule", bare: true, hint: "domain, ip, app or path", list: l, index: -1})
}

func (f field) editable() bool { return f.set != nil || f.list != nil }

func (f field) removable() bool { return f.list != nil && f.index >= 0 }

func (f field) value(s domain.Settings) string {
	if f.list != nil {
		if f.index >= 0 {
			return (*f.list.at(&s))[f.index]
		}
		return ""
	}
	return f.get(s)
}

func (f field) apply(s *domain.Settings, in string) error {
	if f.list == nil {
		return f.set(s, in)
	}
	at := f.list.at(s)
	if in = strings.TrimSpace(in); in == "" {
		if f.index >= 0 {
			*at = slices.Delete(*at, f.index, f.index+1)
		}
		return nil
	}
	rule, err := domain.ParseRule(in)
	if err != nil {
		return err
	}
	if err := conflict(s, at, rule); err != nil {
		return err
	}
	if f.index >= 0 {
		(*at)[f.index] = rule
	} else if !slices.Contains(*at, rule) {
		*at = append(*at, rule)
	}
	return nil
}

func conflict(s *domain.Settings, target *[]string, rule string) error {
	if target == &s.Direct && slices.Contains(s.Proxy, rule) {
		return fmt.Errorf("rule %q already exists in proxy", rule)
	}
	if target == &s.Proxy && slices.Contains(s.Direct, rule) {
		return fmt.Errorf("rule %q already exists in direct", rule)
	}
	return nil
}

func (s *Model) at() (field, bool) {
	rows := s.rows()
	if s.cursor < 0 || s.cursor >= len(rows) {
		return field{}, false
	}
	return rows[s.cursor], true
}

func (s *Model) Dirty() bool {
	return !s.cur.Equal(s.orig)
}

// move skips list headings
func (s *Model) move(delta int) {
	rows := s.rows()
	var editable []int
	for index, row := range rows {
		if row.editable() {
			editable = append(editable, index)
		}
	}
	cursor, _ := slices.BinarySearch(editable, s.cursor)
	s.cursor = editable[min(max(cursor+delta, 0), len(editable)-1)]
	layout, total := s.layout(rows)
	s.reveal(layout, total)
}

func (s *Model) switchTab(delta int) {
	s.tab = (s.tab + delta + len(tabs)) % len(tabs)
	s.cursor, s.scroll, s.err = 0, 0, ""
	s.move(0)
}

func hours(v int) string {
	if v == 0 {
		return ""
	}
	return strconv.Itoa(v) + "h"
}

func parseHours(in string) (int, error) {
	in = strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(in), "h"))
	if in == "" {
		return 0, nil
	}
	v, err := strconv.Atoi(in)
	if err != nil || v < 0 {
		return 0, fmt.Errorf("expected integer hours, got %q", in)
	}
	return v, nil
}
