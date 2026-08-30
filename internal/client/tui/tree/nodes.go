package tree

import (
	"fmt"

	"github.com/luynrs/justray/internal/client/tui/style"
	"github.com/luynrs/justray/internal/client/tui/subscriptions"
	"github.com/luynrs/justray/internal/shared/rpc"
)

func (d Data) Render(r Row, selected bool, width int) string {
	caret := "  "
	if selected {
		caret = style.Strong.Render("❯ ")
	}

	switch r.Kind {
	case Gap:
		return ""
	case Meta:
		return style.Flush("    "+style.Dim.Render(subscriptions.Usage(r.Sub)), subscriptions.Meta(r.Sub, d.Refreshing[r.Sub.ID], d.Spinner), width)
	case Header:
		return caret + subscriptions.Header(r.Sub, d.Collapsed[r.Sub.ID], selected, d.Emoji)
	}
	return caret + style.Flush(d.node(r.Node, selected), info(r.Node), width-2)
}

func (d Data) node(n rpc.Node, selected bool) string {
	name := style.Sanitize(n.Name, d.Emoji)
	if selected {
		name = style.Accent.Render(name)
	}
	line := "  " + d.dot(n) + " " + name
	if lat := latency(n); lat != "" {
		line += " " + style.Dim.Render(lat)
	}
	return line
}

func info(n rpc.Node) string {
	return style.Dim.Render(fmt.Sprintf("%s:%d · %s", style.Sanitize(n.Server, true), n.Port, n.Protocol))
}

func latency(n rpc.Node) string {
	switch {
	case !n.Probed:
		return ""
	case n.Alive:
		return fmt.Sprintf("%dms", n.MS)
	}
	return "timeout"
}

func (d Data) dot(n rpc.Node) string {
	switch {
	case d.connected() && d.Status.NodeRef == n.Ref():
		return style.Strong.Render("●")
	case d.Probing[n.Ref()]:
		return style.Pending.Render("○")
	case !n.Probed:
		return style.Unknown.Render("○")
	case n.Alive:
		return style.Alive.Render("○")
	}
	return style.Dead.Render("○")
}
