package settings

import (
	"cmp"
	"fmt"
	"slices"
	"sort"
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
	hint  string
	get   func(*domain.Settings) string
	set   func(*domain.Settings, string) error
	enum  []string
	multi []field
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
			get:  func(settings *domain.Settings) string { return cmp.Or(settings.Autostart, "unknown") },
			set:  func(s *domain.Settings, in string) error { s.Autostart = in; return nil },
		},
		{
			name: "Auto-refresh",
			hint: "Never",
			get:  func(settings *domain.Settings) string { return hours(settings.RefreshEvery) },
			set: func(s *domain.Settings, in string) error {
				v, err := parseHours(in)
				if err != nil {
					return err
				}
				s.RefreshEvery = v
				return nil
			},
		},
		choice("Show emoji", domain.Toggle, func(settings *domain.Settings) *string { return &settings.Emoji }),
		choice("Force TTY", domain.Toggle, func(settings *domain.Settings) *string { return &settings.ForceTTY }),
		choice("Log level", domain.LogLevels, func(settings *domain.Settings) *string { return &settings.LogLevel }),
		{name: "Node details", multi: []field{
			choice("Address", domain.Toggle, func(settings *domain.Settings) *string { return &settings.Details.Address }),
			choice("Transport", domain.Toggle, func(settings *domain.Settings) *string { return &settings.Details.Transport }),
			choice("Protocol", domain.Toggle, func(settings *domain.Settings) *string { return &settings.Details.Protocol }),
		}},
		text("Probe URL", domain.DefaultProbeURL, func(settings *domain.Settings) *string { return &settings.ProbeURL }),
	}},
	{name: "Connection", fields: []field{
		choice("Proxy type", domain.ProxyTypes, func(settings *domain.Settings) *string { return &settings.ProxyType }),
		number("Proxy port", domain.DefaultPort, func(settings *domain.Settings) *int { return &settings.Port }),
		choice("Preferred IP type", domain.IPVersions, func(settings *domain.Settings) *string { return &settings.IPVersion }),
		choice("Allow LAN", domain.Toggle, func(settings *domain.Settings) *string { return &settings.AllowLAN }),
		choice("Tunnel stack", domain.TunStacks, func(settings *domain.Settings) *string { return &settings.TunStack }),
		number("Tunnel MTU", domain.DefaultTunMTU, func(settings *domain.Settings) *int { return &settings.TunMTU }),
		choice("DNS hijack", domain.Toggle, func(settings *domain.Settings) *string { return &settings.DNSHijack }),
		text("DNS server", domain.DefaultDNS, func(settings *domain.Settings) *string { return &settings.DNS }),
	}},
	{name: "Routing", fields: []field{
		choice("Mode", domain.Modes, func(settings *domain.Settings) *string { return &settings.Mode }),
		choice("Direct LAN", domain.Toggle, func(settings *domain.Settings) *string { return &settings.BypassLocal }),
		choice("Strict route", domain.Toggle, func(settings *domain.Settings) *string { return &settings.TunStrict }),
		choice("Block QUIC", domain.Toggle, func(settings *domain.Settings) *string { return &settings.BlockQUIC }),
	}, lists: []list{
		{"Direct", func(v *domain.Settings) *[]string { return &v.Direct }},
		{"Proxy", func(v *domain.Settings) *[]string { return &v.Proxy }},
		{"Block", func(v *domain.Settings) *[]string { return &v.Block }},
	}},
}

func choice(name string, values []string, at func(*domain.Settings) *string) field {
	field := text(name, "", at)
	field.enum = values
	return field
}

func text(name, def string, at func(*domain.Settings) *string) field {
	return field{
		name: name,
		hint: def,
		get:  func(settings *domain.Settings) string { return *at(settings) },
		set: func(settings *domain.Settings, value string) error {
			*at(settings) = cmp.Or(strings.TrimSpace(value), def)
			return nil
		},
	}
}

func number(name string, def int, at func(*domain.Settings) *int) field {
	return field{
		name: name,
		hint: strconv.Itoa(def),
		get:  func(settings *domain.Settings) string { return strconv.Itoa(*at(settings)) },
		set: func(settings *domain.Settings, in string) error {
			if in = strings.TrimSpace(in); in == "" {
				*at(settings) = def
				return nil
			}
			value, err := strconv.Atoi(in)
			if err != nil {
				return fmt.Errorf("not an integer: %s", in)
			}
			*at(settings) = value
			return nil
		},
	}
}

type Model struct {
	top        int
	tab        int
	cursor     int
	option     int
	scroll     int
	width      int
	height     int
	navigation navigation.Keys
	abandon    bool
	input      textinput.Model
	cur        domain.Settings
	orig       domain.Settings
	err        string
	rows       []field
	layout     []fieldLayout
	editable   []int
}

func New(s domain.Settings, top, width, height int) *Model {
	input := textinput.New()
	input.Prompt = ""
	input.SetStyles(style.Input)
	style.ResizeInput(&input, width-8)
	m := &Model{
		top: top, width: width, height: max(height, 1),
		cur: s.Clone(), orig: s, input: input,
	}
	m.rebuild()
	if len(m.editable) > 0 {
		m.focus(m.editable[0])
	}
	return m
}

func (s *Model) Result() (domain.Settings, domain.Settings, error) {
	if s.abandon || !s.Changed() {
		return s.orig, s.orig, nil
	}
	next, err := s.cur.Normalize()
	if err != nil {
		s.err = style.ErrorText(err)
	}
	return next, s.orig, err
}

func (s *Model) Current() domain.Settings { return s.cur }

func (settings *Model) Changed() bool { return !settings.cur.Equal(settings.orig) }

func (s *Model) Err() string { return s.err }

func (s *Model) Editing() bool { return s.input.Focused() }

func (s *Model) selecting() bool {
	return s.at().options() > 0
}

func (s *Model) CancelNavigation() { s.navigation.Reset() }

func (s *Model) Update(msg tea.Msg) (closed bool, cmd tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		key := msg.String()
		if !s.Editing() {
			return s.key(key)
		}
		switch key {
		case "esc":
			s.err = ""
			s.input.Blur()
			return false, nil
		case "enter":
			if s.assign(s.at(), s.input.Value()) {
				s.input.Blur()
				s.move(0)
			}
			return false, nil
		}
	case tea.MouseMsg:
		s.navigation.Reset()
		return false, s.mouse(msg)
	}
	if s.input.Focused() {
		s.input, cmd = s.input.Update(msg)
		return false, cmd
	}
	return false, nil
}

func (s *Model) key(key string) (bool, tea.Cmd) {
	if motion, handled := s.navigation.Read(key); handled {
		s.navigate(motion)
		return false, nil
	}

	switch key {
	case "esc":
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
		if len(s.at().enum) > 0 {
			s.step(1)
			return false, nil
		}
		return false, s.activate()
	case "space":
		if s.selecting() {
			return false, s.activate()
		}

	case "d":
		if f := s.at(); f.removable() && s.assign(f, "") {
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
	if mouse.X < 0 || mouse.X >= s.width {
		return nil
	}
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
		if y < 0 || y >= s.height {
			return nil
		}
		position := s.scroll + y
		extra := s.extra()
		index := sort.Search(len(s.rows), func(i int) bool { return s.bounds(i, extra).end > position })
		if index == len(s.rows) || !s.rows[index].editable() {
			return nil
		}
		bounds := s.bounds(index, extra)
		if position < bounds.start {
			return nil
		}
		if index != s.cursor {
			s.focus(index)
			s.reveal()
			return nil
		}
		if s.selecting() {
			if position == bounds.start {
				return nil
			}
			s.option = position - bounds.start - 1
		}
		return s.activate()

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
	s.reveal()
	f := s.at()
	if len(f.multi) > 0 {
		value := "on"
		if f.multi[s.option].value(&s.cur) == "on" {
			value = "off"
		}
		s.assign(f.multi[s.option], value)
		return nil
	}
	if len(f.enum) > 0 {
		s.assign(f, f.enum[s.option])
		return nil
	}

	s.err = ""
	s.input.Placeholder = f.hint
	s.input.SetValue(f.value(&s.cur))
	s.input.CursorEnd()
	cmd := s.input.Focus()
	s.clamp()
	return cmd
}

// step cycles an enum choice
func (s *Model) step(delta int) {
	current := s.at()
	switch {
	case len(current.enum) > 0:
		s.option = (slices.Index(current.enum, current.value(&s.cur)) + delta + len(current.enum)) % len(current.enum)
		s.assign(current, current.enum[s.option])
	case len(current.multi) > 0:
		s.option = min(max(s.option+delta, 0), len(current.multi)-1)
	default:
		return
	}
	s.clamp()
}

func (s *Model) focus(cursor int) {
	s.cursor, s.option = cursor, 0
	current := s.at()
	if len(current.enum) > 0 {
		s.option = max(slices.Index(current.enum, current.value(&s.cur)), 0)
	}
}

func (s *Model) reorder(dir int) {
	if f := s.at(); f.removable() && domain.Reorder(*f.list.at(&s.cur), f.index, dir) {
		s.err = ""
		s.focus(s.cursor + dir)
		s.reveal()
	}
}

func (s *Model) assign(f field, v string) bool {
	var err error
	if f.list == nil {
		err = f.set(&s.cur, v)
	} else {
		err = s.setRule(f, v)
	}
	s.err = style.ErrorText(err)
	return err == nil
}

func (s *Model) rebuild() {
	tab := &tabs[s.tab]
	s.rows = append(s.rows[:0], tab.fields...)
	for i := range tab.lists {
		group := &tab.lists[i]
		rules := *group.at(&s.cur)
		s.rows = slices.Grow(s.rows, len(rules)+2)
		s.rows = append(s.rows, field{name: group.title, hint: fmt.Sprintf("(%d)", len(rules))})
		for index := range rules {
			s.rows = append(s.rows, field{hint: "Delete?", list: group, index: index})
		}
		s.rows = append(s.rows, field{name: "Add rule", hint: "Domain, app, path, geoip: or geosite:", list: group, index: -1})
	}

	s.layout = slices.Grow(s.layout[:0], len(s.rows))
	s.editable = slices.Grow(s.editable[:0], len(s.rows))
	position := 0
	for i, row := range s.rows {
		if i > 0 && row.list == nil {
			position++
		}
		end := position + 1
		if row.editable() {
			s.editable = append(s.editable, i)
			if row.list == nil {
				end++
			}
		}
		s.layout = append(s.layout, fieldLayout{position, end})
		position = end
	}
}

func (f field) editable() bool { return f.set != nil || f.list != nil || len(f.multi) > 0 }

func (f field) removable() bool { return f.list != nil && f.index >= 0 }

func (f field) options() int { return max(len(f.enum), len(f.multi)) }

func (f field) value(settings *domain.Settings) string {
	if len(f.multi) > 0 {
		var selected []string
		for _, option := range f.multi {
			if option.value(settings) == "on" {
				selected = append(selected, option.name)
			}
		}
		return cmp.Or(strings.Join(selected, ", "), "None")
	}
	if f.list != nil {
		if f.index >= 0 {
			return (*f.list.at(settings))[f.index]
		}
		return ""
	}
	return f.get(settings)
}

func (s *Model) setRule(f field, in string) error {
	at := f.list.at(&s.cur)
	if in = strings.TrimSpace(in); in == "" {
		if f.index >= 0 {
			*at = slices.Delete(*at, f.index, f.index+1)
			s.rebuild()
		}
		return nil
	}
	rule, err := domain.ParseRule(in)
	if err != nil {
		return err
	}
	conflict := ""
	switch {
	case slices.Contains(*at, rule):
		if f.index < 0 || (*at)[f.index] == rule {
			return nil
		}
		conflict = strings.ToLower(f.list.title)
	case at == &s.cur.Direct && slices.Contains(s.cur.Proxy, rule):
		conflict = "proxy"
	case at == &s.cur.Proxy && slices.Contains(s.cur.Direct, rule):
		conflict = "direct"
	}
	if conflict != "" {
		return fmt.Errorf("rule already exists in %s: %s", conflict, rule)
	}
	if f.index >= 0 {
		(*at)[f.index] = rule
	} else {
		*at = append(*at, rule)
		s.rebuild()
	}
	return nil
}

func (s *Model) at() field { return s.rows[s.cursor] }

func (s *Model) navigate(motion navigation.Motion) {
	switch motion.Action {
	case navigation.Move:
		s.move(motion.Distance(s.height, len(s.editable)))
	case navigation.Reorder:
		s.reorder(motion.Direction)
	case navigation.Line:
		s.focus(s.editable[min(motion.Count-1, len(s.editable)-1)])
		s.reveal()
	case navigation.Last:
		s.focus(s.editable[len(s.editable)-1])
		s.reveal()
	case navigation.Page, navigation.HalfPage:
		extra := s.extra()
		delta := motion.Distance(s.height, s.total(extra))
		target := s.layout[s.cursor].start + delta
		index := sort.Search(len(s.editable), func(i int) bool { return s.bounds(s.editable[i], extra).start >= target })
		if delta < 0 && (index == len(s.editable) || s.bounds(s.editable[index], extra).start != target) {
			index--
		}
		s.focus(s.editable[min(max(index, 0), len(s.editable)-1)])
		s.scroll += delta
		s.reveal()
	case navigation.Scroll:
		s.scrollBy(motion.Distance(s.height, s.total(s.extra())))
	}
}

// move skips list headings
func (s *Model) move(delta int) {
	cursor, _ := slices.BinarySearch(s.editable, s.cursor)
	cursor = s.editable[min(max(cursor+delta, 0), len(s.editable)-1)]
	if cursor != s.cursor {
		s.focus(cursor)
	}
	s.reveal()
}

func (s *Model) switchTab(delta int) {
	s.tab = (s.tab + delta + len(tabs)) % len(tabs)
	s.scroll, s.err = 0, ""
	s.rebuild()
	if len(s.editable) > 0 {
		s.focus(s.editable[0])
	}
	s.reveal()
}

func hours(v int) string {
	if v == 0 {
		return ""
	}
	return strconv.Itoa(v) + "h"
}

func parseHours(in string) (int, error) {
	if in = strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(in), "h")); in == "" {
		return 0, nil
	}
	v, err := strconv.Atoi(in)
	if err != nil {
		return 0, fmt.Errorf("not an integer: %s", in)
	}
	if v < 0 {
		return 0, fmt.Errorf("invalid refresh interval: %s", in)
	}
	return v, nil
}
