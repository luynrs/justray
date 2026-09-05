package tree

import (
	"cmp"
	"fmt"

	"github.com/luynrs/justray/internal/client/tui/style"
	"github.com/luynrs/justray/internal/ipc"
)

func (d Data) Render(r Row, selected bool, width int) string {
	bar := "  "
	if selected {
		bar = style.Accent.Render("▎ ")
	}

	switch r.Kind {
	case Gap:
		return ""
	case Meta:
		return bar + style.Flush("  "+style.Usage(r.Sub.Traffic), subMeta(r.Sub, d.Spinner), width-2)
	case Header:
		return bar + subHeader(r.Sub, d.Collapsed[r.Sub.ID], selected, d.Emoji)
	}
	return bar + style.Flush(d.node(r.Node, selected), info(r.Node), width-2)
}

func subHeader(s ipc.Sub, collapsed, selected, emoji bool) string {
	arrow := "▼"
	if collapsed {
		arrow = "▶"
	}
	clean := style.Sanitize(s.Name, emoji)
	if selected {
		return style.Strong.Render(arrow + " " + clean)
	}
	return arrow + " " + style.Name.Render(clean)
}

func subMeta(s ipc.Sub, spinner string) string {
	age := "never updated"
	switch {
	case s.Refreshing:
		age = "updated " + spinner + " ago"
	case !s.UpdatedAt.IsZero():
		age = "updated " + style.Since(s.UpdatedAt)
	}
	plural := "s"
	if s.Nodes == 1 {
		plural = ""
	}
	return style.Dim.Render(fmt.Sprintf("%d node%s · %s", s.Nodes, plural, age))
}

func (d Data) node(n ipc.Node, selected bool) string {
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

func info(n ipc.Node) string {
	return style.Dim.Render(fmt.Sprintf("%s:%d · %s", style.Sanitize(n.Server, true), n.Port, n.Protocol))
}

func latency(n ipc.Node) string {
	switch {
	case n.Probing || !n.Probed:
		return ""
	case n.Alive:
		return fmt.Sprintf("%dms", n.MS)
	}
	return "fail"
}

func (d Data) dot(n ipc.Node) string {
	switch {
	case d.connected() && d.Status.NodeRef == n.Ref():
		return style.Alive.Render("●")
	case n.Probing:
		return style.Pending.Render(cmp.Or(d.Spinner, "○"))
	case !n.Probed:
		return style.Unknown.Render("○")
	case n.Alive:
		return style.Alive.Render("○")
	}
	return style.Dead.Render("○")
}
