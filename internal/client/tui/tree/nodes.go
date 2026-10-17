package tree

import (
	"fmt"
	"slices"

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
	return bar + style.Flush(d.node(r.Node, selected), info(r.Node), width-2)
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

func info(n ipc.Node) string {
	return style.Gray.Render(fmt.Sprintf("%s %s %s", domain.ServerAddress(style.Sanitize(n.Server, true), n.Port), style.Sep(), n.Protocol))
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
