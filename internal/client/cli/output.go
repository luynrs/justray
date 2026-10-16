package cli

import (
	"fmt"
	"os"
	"strings"
	"time"

	"charm.land/lipgloss/v2"

	"github.com/luynrs/justray/internal/client/tui/style"
	"github.com/luynrs/justray/internal/domain"
	"github.com/luynrs/justray/internal/ipc"
)

func out(s string) { _, _ = lipgloss.Println(s) }

func done(text string) { out(style.Green.Bold(true).Render(style.Check()) + " " + text) }

func fields(pairs ...[2]string) {
	w := 0
	for _, p := range pairs {
		w = max(w, lipgloss.Width(p[0]))
	}
	lines := make([]string, len(pairs))
	for i, p := range pairs {
		lines[i] = "  " + style.Pad(style.Gray.Render(p[0]+":"), w+1) + " " + p[1]
	}
	out(strings.Join(lines, "\n"))
}

func state(st ipc.Status) string {
	if st.Connected {
		text := "Connected via " + strings.ToUpper(modeWord(st.Tun))
		if uptime := st.Uptime(); uptime > 0 {
			text += " for " + style.Uptime(uptime)
		}
		return text
	}
	return "Disconnected"
}

func stateHeadline(st ipc.Status) {
	text := state(st)
	if st.Connected {
		done(text)
		return
	}
	out(style.Gray.Render(style.Sep()) + " " + text)
}

func (a *app) nodeDetails(st ipc.Status, nodes []ipc.Node) {
	n := lookup(st.NodeRef, nodes)
	pairs := [][2]string{{"Node", a.nodeName(st.NodeName, st.NodeRef.NodeID)}}
	if !st.Tun && st.Port > 0 {
		pairs = append(pairs, [2]string{"Proxy", fmt.Sprintf("127.0.0.1:%d", st.Port)})
	}
	fields(append(pairs, a.nodeFields(n)...)...)
}

func lookup(ref domain.NodeRef, nodes []ipc.Node) ipc.Node {
	for _, n := range nodes {
		if n.NodeID == ref.NodeID && (ref.SubscriptionID == "" || n.SubscriptionID == ref.SubscriptionID) {
			return n
		}
	}
	return ipc.Node{}
}

func (a *app) nodeFields(n ipc.Node) [][2]string {
	if n.Server == "" {
		return nil
	}
	return [][2]string{
		{"Server", domain.ServerAddress(a.clean(n.Server), n.Port)},
		{"Protocol", n.Protocol},
	}
}

func (a *app) nodeName(name, id string) string {
	if id == "" {
		return a.clean(name)
	}
	return a.clean(name) + " " + style.Gray.Render("("+displayID(id)+")")
}

func displayID(id string) string { return id[:min(8, len(id))] }

func modeWord(tun bool) string {
	if tun {
		return "tun"
	}
	return "proxy"
}

func (application *app) fail(err error) {
	if message := style.ErrorText(err); message != "" {
		_, _ = lipgloss.Fprintln(os.Stderr, style.Red.Bold(true).Render(style.Cross()), application.clean(message))
	}
}

func (a *app) clean(s string) string { return style.Sanitize(s, a.emoji) }

func spin(text string) func() {
	if fi, err := os.Stdout.Stat(); err != nil || fi.Mode()&os.ModeCharDevice == 0 {
		return func() {}
	}
	s := style.Spinner()
	quit, stopped := make(chan struct{}), make(chan struct{})
	go func() {
		defer close(stopped)
		for i := 0; ; i++ {
			_, _ = lipgloss.Printf("\r%s %s", style.Cyan.Render(s.Frames[i%len(s.Frames)]), text)
			select {
			case <-quit:
				return
			case <-time.After(s.FPS):
			}
		}
	}()
	return func() {
		close(quit)
		<-stopped
		_, _ = lipgloss.Print("\r\x1b[K")
	}
}
