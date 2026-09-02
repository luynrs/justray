package tree

import (
	"strings"

	"github.com/luynrs/justray/internal/shared/domain"
	"github.com/luynrs/justray/internal/shared/rpc"
)

type Kind int

const (
	Header Kind = iota
	Node
	Gap
	Meta
)

type Row struct {
	Kind Kind
	Sub  rpc.Sub
	Node rpc.Node
}

func (r Row) SubID() string {
	return r.Sub.ID
}

func (r Row) Selectable() bool { return r.Kind == Header || r.Kind == Node }

type Data struct {
	Subs       []rpc.Sub
	Nodes      []rpc.Node
	Collapsed  map[string]bool
	Probing    map[domain.NodeRef]bool
	Refreshing map[string]bool
	Query      string
	Status     rpc.Status
	Live       bool
	Emoji      bool
	Spinner    string
}

func (d Data) connected() bool { return d.Live && d.Status.Connected }

const Default = "default"

type Group struct {
	Sub   rpc.Sub
	Nodes []rpc.Node
}

func (d Data) Groups() []Group {
	index := make(map[string][]rpc.Node, len(d.Subs))
	for _, n := range d.Nodes {
		index[n.Sub] = append(index[n.Sub], n)
	}

	out := make([]Group, 0, len(d.Subs))
	var loose []rpc.Node
	for _, sub := range d.Subs {
		nodes := index[sub.ID]
		if sub.Direct {
			loose = append(loose, nodes...)
			continue
		}
		out = append(out, Group{Sub: sub, Nodes: nodes})
	}
	if len(loose) > 0 {
		out = append(out, Group{Sub: rpc.Sub{Name: "Default", ID: Default}, Nodes: loose})
	}
	return out
}

func (d Data) Rows() []Row {
	q := strings.ToLower(strings.TrimSpace(d.Query))
	subs := make(map[string]rpc.Sub, len(d.Subs))
	for _, sub := range d.Subs {
		subs[sub.ID] = sub
	}

	var rows []Row
	for _, g := range d.Groups() {
		nodes := g.Nodes
		if q != "" && !strings.Contains(strings.ToLower(g.Sub.Name), q) {
			nodes = matching(nodes, q)
			if len(nodes) == 0 {
				continue
			}
		}
		if len(rows) > 0 {
			rows = append(rows, Row{Kind: Gap})
		}

		rows = append(rows, Row{Kind: Header, Sub: g.Sub})
		if g.Sub.ID != Default {
			rows = append(rows, Row{Kind: Meta, Sub: g.Sub})
		}
		for _, n := range nodes {
			if q != "" || !d.Collapsed[g.Sub.ID] || (d.connected() && d.Status.NodeRef == n.Ref()) {
				rows = append(rows, Row{Kind: Node, Sub: subs[n.Sub], Node: n})
			}
		}
	}
	return rows
}

func matching(nodes []rpc.Node, q string) []rpc.Node {
	var out []rpc.Node
	for _, n := range nodes {
		if strings.Contains(strings.ToLower(n.Name+" "+n.Protocol+" "+n.Server), q) {
			out = append(out, n)
		}
	}
	return out
}

func Selectable(rows []Row) []int {
	var out []int
	for i, r := range rows {
		if r.Selectable() {
			out = append(out, i)
		}
	}
	return out
}

func At(rows []Row, cursor int) (Row, bool) {
	sel := Selectable(rows)
	if cursor < 0 || cursor >= len(sel) {
		return Row{}, false
	}
	return rows[sel[cursor]], true
}

func Clamp(rows []Row, cursor, scroll, height int) (int, int) {
	sel := Selectable(rows)
	if len(sel) == 0 {
		return 0, 0
	}
	cursor = min(max(cursor, 0), len(sel)-1)

	pos := sel[cursor]
	if pos < scroll {
		scroll = pos
	}
	if pos >= scroll+height {
		scroll = pos - height + 1
	}
	return cursor, min(max(scroll, 0), max(len(rows)-height, 0))
}

// Point maps a screen line to a cursor position
func Point(rows []Row, scroll, height, top, y int) (cursor int, ok bool) {
	i := scroll + y - top
	if y < top || y >= top+height || i < 0 || i >= len(rows) || !rows[i].Selectable() {
		return 0, false
	}
	return len(Selectable(rows[:i])), true
}
