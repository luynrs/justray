package tree

import (
	"slices"
	"strings"

	"github.com/luynrs/justray/internal/domain"
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
	Sub  *ipc.Subscription
	Node *ipc.Node
}

func (r Row) Removable() bool {
	return r.Kind == Header || r.Kind == Node && r.Sub.SubscriptionID == "default" && !r.Sub.Refreshable
}

type Data struct {
	Collapsed []string
	Status    ipc.Status
	Live      bool
	Emoji     bool
	Spinner   string
	Details   domain.Details
}

func (d Data) active(n ipc.Node) bool {
	return d.Live && d.Status.Connected && d.Status.NodeRef == n.Ref()
}

type Group struct {
	Sub    *ipc.Subscription
	Nodes  []*ipc.Node
	search []string
}

func Groups(subs []ipc.Subscription, nodes []ipc.Node) []Group {
	if len(subs) == 0 {
		return nil
	}
	index := make(map[string][]*ipc.Node, len(subs))
	for i := range nodes {
		node := &nodes[i]
		index[node.SubscriptionID] = append(index[node.SubscriptionID], node)
	}
	groups := make([]Group, len(subs))
	for i := range subs {
		groups[i] = Group{Sub: &subs[i], Nodes: index[subs[i].SubscriptionID]}
	}
	return groups
}

type List struct {
	Rows       []Row
	Selectable []int
}

func (d Data) List(groups []Group, query string) List {
	query = strings.ToLower(strings.TrimSpace(query))
	capacity := len(groups)
	if query == "" && len(d.Collapsed) == 0 {
		for i := range groups {
			capacity += len(groups[i].Nodes)
		}
	}
	rows := make([]Row, 0, capacity+2*len(groups))
	selectable := make([]int, 0, capacity)
	for i := range groups {
		group := &groups[i]
		sub := group.Sub
		start, first := len(rows), len(selectable)
		if len(rows) > 0 {
			rows = append(rows, Row{Kind: Gap})
		}

		selectable = append(selectable, len(rows))
		rows = append(rows, Row{Kind: Header, Sub: sub})
		if sub.Refreshable {
			rows = append(rows, Row{Kind: Meta, Sub: sub})
		}
		filtered := query != "" && !strings.Contains(strings.ToLower(sub.Name), query)
		collapsed := query == "" && slices.Contains(d.Collapsed, sub.SubscriptionID)
		if filtered && group.search == nil {
			group.search = make([]string, len(group.Nodes))
			for j, node := range group.Nodes {
				group.search[j] = strings.ToLower(node.Name + " " + node.Protocol + " " + node.Server)
			}
		}
		for j, node := range group.Nodes {
			if filtered && !strings.Contains(group.search[j], query) {
				continue
			}
			if !collapsed || d.active(*node) {
				selectable = append(selectable, len(rows))
				rows = append(rows, Row{Kind: Node, Sub: sub, Node: node})
			}
		}
		if filtered && len(selectable) == first+1 {
			rows = rows[:start]
			selectable = selectable[:first]
		}
	}
	return List{Rows: rows, Selectable: selectable}
}

func (l List) Position(cursor int) int {
	if cursor >= 0 && cursor < len(l.Selectable) {
		return l.Selectable[cursor]
	}
	return -1
}

func (l List) At(cursor int) (Row, bool) {
	if position := l.Position(cursor); position >= 0 {
		return l.Rows[position], true
	}
	return Row{}, false
}

func (l List) Clamp(cursor, scroll, height int) (int, int) {
	return min(max(cursor, 0), max(len(l.Selectable)-1, 0)), min(max(scroll, 0), max(len(l.Rows)-height, 0))
}

func (l List) Reveal(cursor, scroll, height int) (int, int) {
	if len(l.Selectable) == 0 {
		return 0, 0
	}
	cursor, scroll = l.Clamp(cursor, scroll, height)
	pos := l.Selectable[cursor]
	return cursor, min(max(scroll, pos-height+1), pos)
}

func (l List) Point(i int) (int, bool) {
	if i < 0 || i >= len(l.Rows) {
		return 0, false
	}
	if l.Rows[i].Kind == Meta {
		i--
	}
	return slices.BinarySearch(l.Selectable, i)
}
