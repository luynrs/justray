package tree

import (
	"fmt"
	"slices"
	"strings"

	"github.com/luynrs/justray/internal/client/tui/style"
	"github.com/luynrs/justray/internal/domain"
	"github.com/luynrs/justray/internal/ipc"
)

func (d Data) Render(r Row, selected bool, width int) string {
	bar := "  "
	if selected {
		bar = style.Cyan.Render(style.Bar())
	}

	switch r.Kind {
	case Gap:
		return ""
	case Meta:
		return bar + style.Flush("  "+style.Usage(domain.Traffic(r.Sub.Traffic)), subMeta(r.Sub, d.Spinner), width-2)
	case Header:
		return bar + subHeader(r.Sub, slices.Contains(d.Collapsed, r.Sub.SubscriptionID), selected, d.Emoji)
	}
	return bar + style.Flush(d.node(r.Node, selected), d.info(r.Node), width-2)
}

func subHeader(s ipc.Subscription, collapsed, selected, emoji bool) string {
	arrow := style.Arrow(collapsed)
	clean := style.Sanitize(s.Name, emoji)
	if selected {
		return style.Cyan.Bold(true).Render(arrow + " " + clean)
	}
	return arrow + " " + style.Bold.Render(clean)
}

func subMeta(s ipc.Subscription, spinner string) string {
	age := "never updated"
	switch {
	case s.Refreshing:
		age = "updated " + spinner + " ago"
	case !s.UpdatedAt.IsZero():
		age = "updated " + style.Since(s.UpdatedAt)
	}
	plural := "s"
	if s.NodeCount == 1 {
		plural = ""
	}
	return style.Gray.Render(fmt.Sprintf("%d node%s %s %s", s.NodeCount, plural, style.Sep(), age))
}

func (d Data) node(n ipc.Node, selected bool) string {
	name := style.Sanitize(n.Name, d.Emoji)
	if selected {
		name = style.Cyan.Render(name)
	}
	line := "  " + d.dot(n) + " " + name
	if lat := n.Latency(); lat != "" {
		line += " " + style.Gray.Render(lat)
	}
	return line
}

func (d Data) info(node ipc.Node) string {
	var parts []string
	if d.Details.Address == "on" && node.Server != "" {
		parts = append(parts, domain.ServerAddress(style.Sanitize(node.Server, true), node.Port))
	}
	if d.Details.Transport == "on" && node.Transport != "" {
		parts = append(parts, style.Sanitize(node.Transport, true))
	}
	if d.Details.Protocol == "on" && node.Protocol != "" {
		parts = append(parts, style.Sanitize(node.Protocol, true))
	}
	return style.Gray.Render(strings.Join(parts, " "+style.Sep()+" "))
}

func (d Data) dot(n ipc.Node) string {
	switch {
	case d.connected() && d.Status.NodeRef == n.Ref():
		return style.Green.Render(style.Dot(true))
	case n.Probing:
		return style.Yellow.Render(d.Spinner)
	case !n.Probed:
		return style.Gray.Render(style.Dot(false))
	case n.Alive:
		return style.Green.Render(style.Dot(false))
	}
	return style.Red.Render(style.Dot(false))
}
