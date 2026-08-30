package tree

import (
	"fmt"
	"strings"

	"github.com/luynrs/justray/internal/client/tui/style"
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
		return style.Flush("    "+style.Dim.Render(subUsage(r.Sub)), subMeta(r.Sub, d.Refreshing[r.Sub.ID], d.Spinner), width)
	case Header:
		return caret + subHeader(r.Sub, d.Collapsed[r.Sub.ID], selected, d.Emoji)
	}
	return caret + style.Flush(d.node(r.Node, selected), info(r.Node), width-2)
}

func subHeader(s rpc.Sub, collapsed, selected, emoji bool) string {
	arrow := "▼"
	if collapsed {
		arrow = "▶"
	}
	clean := style.Sanitize(s.Name, emoji)
	name := style.Name.Render(clean)
	if selected {
		name = style.Strong.Render(clean)
	}
	return arrow + " " + name
}

func subMeta(s rpc.Sub, refreshing bool, spinner string) string {
	age := "never updated"
	switch {
	case refreshing:
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

func subUsage(s rpc.Sub) string {
	t := s.Traffic
	used := t.UploadBytes + t.DownloadBytes

	var parts []string
	switch {
	case t.TotalBytes > 0:
		parts = append(parts, fmt.Sprintf("%s %s %s",
			style.Bytes(used),
			style.Bar(float64(used)/float64(t.TotalBytes)),
			style.Bytes(t.TotalBytes)))
	case used > 0:
		parts = append(parts, style.Bytes(used)+" used")
	default:
		parts = append(parts, "No data")
	}
	if !t.ExpiresAt.IsZero() {
		parts = append(parts, style.Expiry(t.ExpiresAt))
	}
	return strings.Join(parts, " · ")
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
