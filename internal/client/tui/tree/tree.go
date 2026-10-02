package tree

import (
	"slices"
	"strings"

	"github.com/luynrs/justray/internal/ipc"
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
	Sub  ipc.Subscription
	Node ipc.Node
}

func (r Row) Selectable() bool { return r.Kind == Header || r.Kind == Node }

func (r Row) Removable() bool {
	return r.Kind == Header || r.Kind == Node && r.Sub.SubscriptionID == "default" && !r.Sub.Refreshable
}

type Data struct {
	Subs      []ipc.Subscription
	Nodes     []ipc.Node
	Collapsed []string
	Query     string
	Status    ipc.Status
	Live      bool
	Emoji     bool
	Spinner   string
}

func (d Data) connected() bool { return d.Live && d.Status.Connected }

type Group struct {
	Sub   ipc.Subscription
	Nodes []ipc.Node
}

func (d Data) Groups() []Group {
	index := make(map[string][]ipc.Node, len(d.Subs))
	for _, node := range d.Nodes {
		index[node.SubscriptionID] = append(index[node.SubscriptionID], node)
	}
	groups := make([]Group, 0, len(d.Subs))
	for _, sub := range d.Subs {
		groups = append(groups, Group{Sub: sub, Nodes: index[sub.SubscriptionID]})
	}
	return groups
}

func (d Data) Rows() []Row {
	q := strings.ToLower(strings.TrimSpace(d.Query))
	var rows []Row
	for _, group := range d.Groups() {
		nodes := group.Nodes
		if q != "" && !strings.Contains(strings.ToLower(group.Sub.Name), q) {
			nodes = matching(nodes, q)
			if len(nodes) == 0 {
				continue
			}
		}
		if len(rows) > 0 {
			rows = append(rows, Row{Kind: Gap})
		}

		rows = append(rows, Row{Kind: Header, Sub: group.Sub})
		if group.Sub.Refreshable {
			rows = append(rows, Row{Kind: Meta, Sub: group.Sub})
		}
		collapsed := slices.Contains(d.Collapsed, group.Sub.SubscriptionID)
		for _, n := range nodes {
			if q != "" || !collapsed || (d.connected() && d.Status.NodeRef == n.Ref()) {
				rows = append(rows, Row{Kind: Node, Sub: group.Sub, Node: n})
			}
		}
	}
	return rows
}

func matching(nodes []ipc.Node, q string) []ipc.Node {
	var out []ipc.Node
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
	if y < top || y >= top+height || i < 0 || i >= len(rows) {
		return 0, false
	}
	if rows[i].Kind == Meta && i > 0 && rows[i-1].Kind == Header {
		i--
	}
	if !rows[i].Selectable() {
		return 0, false
	}
	return len(Selectable(rows[:i])), true
}
