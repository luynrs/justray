package settings

import (
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"

	"github.com/luynrs/justray/internal/shared/domain"
)

type field struct {
	name   string
	bare   bool   // the row is its own value, like a routing rule
	header bool   // a list heading, not a setting
	hint   string // the editor placeholder, and the value shown while unset
	get    func(domain.Settings) string
	set    func(*domain.Settings, string) error
	enum   []string
	remove func(*domain.Settings)
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
			name: "Start at login",
			enum: domain.Toggle,
			get:  func(s domain.Settings) string { return s.Autostart },
			set:  func(s *domain.Settings, in string) error { s.Autostart = in; return nil },
		},
		{
			name: "Refresh subscriptions",
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
			name: "Mixed port",
			get:  func(s domain.Settings) string { return strconv.Itoa(s.Port) },
			set:  setInt(func(s *domain.Settings) *int { return &s.Port }),
		},
		{
			name: "Unicode emoji",
			enum: domain.Toggle,
			get:  func(s domain.Settings) string { return s.Emoji },
			set:  func(s *domain.Settings, in string) error { s.Emoji = in; return nil },
		},
		{
			name: "Logging",
			enum: domain.LogLevels,
			get:  func(s domain.Settings) string { return s.LogLevel },
			set:  func(s *domain.Settings, in string) error { s.LogLevel = in; return nil },
		},
		{
			name: "Probe URL",
			get:  func(s domain.Settings) string { return s.ProbeURL },
			set:  func(s *domain.Settings, in string) error { s.ProbeURL = strings.TrimSpace(in); return nil },
		},
	}},
	{name: "Network", fields: []field{
		{
			name: "DNS hijack",
			enum: domain.Toggle,
			get:  func(s domain.Settings) string { return s.DNSHijack },
			set:  func(s *domain.Settings, in string) error { s.DNSHijack = in; return nil },
		},
		{
			name: "DNS server",
			get:  func(s domain.Settings) string { return s.DNS },
			set:  func(s *domain.Settings, in string) error { s.DNS = strings.TrimSpace(in); return nil },
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
			get:  func(s domain.Settings) string { return strconv.Itoa(s.TunMTU) },
			set:  setInt(func(s *domain.Settings) *int { return &s.TunMTU }),
		},
		{
			name: "Strict route",
			enum: domain.Toggle,
			get:  func(s domain.Settings) string { return s.TunStrict },
			set:  func(s *domain.Settings, in string) error { s.TunStrict = in; return nil },
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
			name: "Block QUIC",
			enum: domain.Toggle,
			get:  func(s domain.Settings) string { return s.BlockQUIC },
			set:  func(s *domain.Settings, in string) error { s.BlockQUIC = in; return nil },
		},
	}, lists: []list{
		{"Except", func(v *domain.Settings) *[]string { return &v.Except }},
		{"Blocked", func(v *domain.Settings) *[]string { return &v.Blocked }},
	}},
}

func setInt(at func(*domain.Settings) *int) func(*domain.Settings, string) error {
	return func(s *domain.Settings, in string) error {
		v, err := strconv.Atoi(strings.TrimSpace(in))
		if err != nil {
			return fmt.Errorf("%q is not a number", in)
		}
		*at(s) = v
		return nil
	}
}

type Settings struct {
	top     int
	hits    map[int]hit
	tab     int
	cursor  int
	scroll  int
	editing bool
	abandon bool
	input   textinput.Model
	cur     domain.Settings
	orig    domain.Settings
	err     string
	wheel   time.Time
}

func New(s domain.Settings, top int) *Settings {
	input := textinput.New()
	input.CharLimit = 64
	return &Settings{top: top, cur: s, orig: s, input: input}
}

func (s *Settings) Result() (domain.Settings, bool, error) {
	if s.abandon || !s.dirty() {
		return s.orig, false, nil
	}
	next, err := s.cur.Normalize()
	return next, true, err
}

func (s *Settings) Update(msg tea.Msg) (closed bool, cmd tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		return s.key(msg)
	case tea.MouseMsg:
		return false, s.mouse(msg)
	case tea.PasteMsg:
		if s.editing {
			var cmd tea.Cmd
			s.input, cmd = s.input.Update(msg)
			return false, cmd
		}
	}
	return false, nil
}

func (s *Settings) key(msg tea.KeyPressMsg) (bool, tea.Cmd) {
	if s.editing {
		return false, s.editKey(msg)
	}

	switch msg.String() {
	case "esc", "q":
		// first esc reports the problem, a second one leaves without saving
		if s.err != "" {
			s.abandon = true
			return true, nil
		}
		if _, _, err := s.Result(); err != nil {
			s.err = err.Error()
			return false, nil
		}
		return true, nil

	case "right", "l":
		s.step(1)
	case "left", "h":
		s.step(-1)
	case "tab":
		s.switchTab(1)
	case "shift+tab":
		s.switchTab(-1)
	case "up", "k":
		s.move(-1)
	case "down", "j":
		s.move(1)

	case "enter":
		return false, s.activate()

	case "d":
		if f, ok := s.at(); ok && f.remove != nil {
			f.remove(&s.cur)
			s.move(0)
		}
	}
	return false, nil
}

func (s *Settings) mouse(msg tea.MouseMsg) tea.Cmd {
	if s.editing {
		return nil
	}
	mouse := msg.Mouse()
	y := mouse.Y - s.top

	switch msg.(type) {
	case tea.MouseClickMsg:
		if mouse.Button != tea.MouseLeft {
			return nil
		}
		if y == 0 {
			if i, ok := tabAt(mouse.X); ok {
				s.switchTab(i - s.tab)
			}
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
		if rows := s.rows(); h.row < len(rows) && rows[h.row].header {
			return nil
		}
		if h.row == s.cursor {
			return s.activate()
		}
		s.cursor = h.row

	case tea.MouseWheelMsg:
		// one physical notch fires several of these; count it once
		if time.Since(s.wheel) < 20*time.Millisecond {
			return nil
		}
		s.wheel = time.Now()
		switch mouse.Button {
		case tea.MouseWheelUp:
			s.move(-1)
		case tea.MouseWheelDown:
			s.move(1)
		}
	}
	return nil
}

func (s *Settings) activate() tea.Cmd {
	f, ok := s.at()
	if !ok || f.set == nil {
		return nil
	}
	if len(f.enum) > 0 {
		s.step(1)
		return nil
	}

	s.editing, s.err = true, ""
	s.input.Placeholder = f.hint
	s.input.SetValue(f.get(s.cur))
	s.input.CursorEnd()
	return s.input.Focus()
}

func (s *Settings) editKey(msg tea.KeyPressMsg) tea.Cmd {
	switch msg.String() {
	case "esc":
		s.editing, s.err = false, ""
		s.input.Blur()
		return nil

	case "enter":
		f, ok := s.at()
		if !ok {
			s.editing = false
			return nil
		}
		if err := f.set(&s.cur, s.input.Value()); err != nil {
			s.err = err.Error()
			return nil
		}
		s.editing, s.err = false, ""
		s.input.Blur()
		s.move(0)
		return nil
	}

	var cmd tea.Cmd
	s.input, cmd = s.input.Update(msg)
	return cmd
}

// step cycles an enum choice
func (s *Settings) step(delta int) {
	f, ok := s.at()
	if !ok || len(f.enum) == 0 {
		return
	}
	i := (slices.Index(f.enum, f.get(s.cur)) + delta + len(f.enum)) % len(f.enum)
	s.assign(f, f.enum[i])
}

func (s *Settings) assign(f field, v string) {
	s.err = ""
	if f.set == nil {
		return
	}
	if err := f.set(&s.cur, v); err != nil {
		s.err = err.Error()
	}
}

func (s *Settings) rows() []field {
	t := tabs[s.tab]
	out := slices.Clone(t.fields)
	for _, l := range t.lists {
		out = append(out, s.listRows(l)...)
	}
	return out
}

// listRows is a heading, its entries and an add row
func (s *Settings) listRows(l list) []field {
	entries := *l.at(&s.cur)
	out := make([]field, 0, len(entries)+2)
	out = append(out, field{name: l.title, header: true})

	for i := range entries {
		set := func(v *domain.Settings, in string) error {
			at := l.at(v)
			if in = strings.TrimSpace(in); in == "" {
				*at = append((*at)[:i:i], (*at)[i+1:]...)
				return nil
			}
			rule, err := domain.ParseRule(in)
			if err != nil {
				return err
			}
			*at = append(append((*at)[:i:i], rule), (*at)[i+1:]...)
			return nil
		}
		out = append(out, field{
			name:   entries[i],
			bare:   true,
			hint:   "empty removes it",
			get:    func(v domain.Settings) string { return (*l.at(&v))[i] },
			set:    set,
			remove: func(v *domain.Settings) { _ = set(v, "") }, // empty input never errors
		})
	}

	return append(out, field{
		name: "+ add domain, network or app",
		bare: true,
		hint: "example.com, 2ip.*, 10.0.0.0/8, firefox",
		get:  func(domain.Settings) string { return "" },
		set: func(v *domain.Settings, in string) error {
			if in = strings.TrimSpace(in); in == "" {
				return nil
			}
			rule, err := domain.ParseRule(in)
			if err != nil {
				return err
			}
			*l.at(v) = append(*l.at(v), rule)
			return nil
		},
	})
}

func (s *Settings) at() (field, bool) {
	rows := s.rows()
	if s.cursor < 0 || s.cursor >= len(rows) {
		return field{}, false
	}
	return rows[s.cursor], true
}

func (s *Settings) dirty() bool {
	return !s.cur.Equal(s.orig)
}

// move skips list headings
func (s *Settings) move(delta int) {
	rows := s.rows()
	if len(rows) == 0 {
		s.cursor = 0
		return
	}

	i := min(max(s.cursor+delta, 0), len(rows)-1)
	step := max(min(delta, 1), -1)
	if step == 0 {
		step = 1
	}
	for n := 0; n < len(rows) && rows[i].header; n++ {
		next := i + step
		if next < 0 || next >= len(rows) {
			step = -step
			next = min(max(i+step, 0), len(rows)-1)
		}
		i = next
	}
	s.cursor = i
}

func (s *Settings) switchTab(delta int) {
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
		return 0, fmt.Errorf("%q is not a number of hours", in)
	}
	return v, nil
}
