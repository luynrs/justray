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
	Nodes []*ipc.Node
}

func (d Data) Groups() []Group {
	index := make(map[string][]*ipc.Node, len(d.Subs))
	for i := range d.Nodes {
		node := &d.Nodes[i]
		index[node.SubscriptionID] = append(index[node.SubscriptionID], node)
	}
	groups := make([]Group, 0, len(d.Subs))
	for _, sub := range d.Subs {
		groups = append(groups, Group{Sub: sub, Nodes: index[sub.SubscriptionID]})
	}
	return groups
}

func (d Data) Rows() []Row {
	query := strings.ToLower(strings.TrimSpace(d.Query))
	var rows []Row
	for _, group := range d.Groups() {
		start := len(rows)
		if len(rows) > 0 {
			rows = append(rows, Row{Kind: Gap})
		}

		rows = append(rows, Row{Kind: Header, Sub: group.Sub})
		if group.Sub.Refreshable {
			rows = append(rows, Row{Kind: Meta, Sub: group.Sub})
		}
		nodeStart := len(rows)
		filtered := query != "" && !strings.Contains(strings.ToLower(group.Sub.Name), query)
		collapsed := slices.Contains(d.Collapsed, group.Sub.SubscriptionID)
		for _, node := range group.Nodes {
			if filtered && !strings.Contains(strings.ToLower(node.Name+" "+node.Protocol+" "+node.Server), query) {
				continue
			}
			if query != "" || !collapsed || (d.connected() && d.Status.NodeRef == node.Ref()) {
				rows = append(rows, Row{Kind: Node, Sub: group.Sub, Node: *node})
			}
		}
		if filtered && len(rows) == nodeStart {
			rows = rows[:start]
		}
	}
	return rows
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
	if cursor < 0 {
		return Row{}, false
	}
	for _, row := range rows {
		if row.Selectable() {
			if cursor == 0 {
				return row, true
			}
			cursor--
		}
	}
	return Row{}, false
}

func Clamp(rows []Row, cursor, scroll, height int) (int, int) {
	count := 0
	for _, row := range rows {
		if row.Selectable() {
			count++
		}
	}
	return min(max(cursor, 0), max(count-1, 0)), min(max(scroll, 0), max(len(rows)-height, 0))
}

func Reveal(rows []Row, cursor, scroll, height int) (int, int) {
	cursor = max(cursor, 0)
	count, pos := 0, 0
	for i, row := range rows {
		if row.Selectable() {
			if count <= cursor {
				pos = i
			}
			count++
		}
	}
	if count == 0 {
		return 0, 0
	}
	cursor = min(cursor, count-1)
	if pos < scroll {
		scroll = pos
	}
	if pos >= scroll+height {
		scroll = pos - height + 1
	}
	return cursor, min(max(scroll, 0), max(len(rows)-height, 0))
}

func Page(rows []Row, cursor, scroll, delta, height int) (int, int) {
	selectable := Selectable(rows)
	if len(selectable) == 0 {
		return 0, 0
	}
	cursor, exact := slices.BinarySearch(selectable, selectable[min(max(cursor, 0), len(selectable)-1)]+delta)
	if delta < 0 && !exact {
		cursor--
	}
	return Reveal(rows, cursor, scroll+delta, height)
}

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
	for _, row := range rows[:i] {
		if row.Selectable() {
			cursor++
		}
	}
	return cursor, true
}
