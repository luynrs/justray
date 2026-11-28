package tui

import (
	"image"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"

	"github.com/luynrs/justray/internal/client/tui/navigation"
	"github.com/luynrs/justray/internal/client/tui/style"
	"github.com/luynrs/justray/internal/client/tui/tree"
)

type modalKind uint8

const (
	modalNone modalKind = iota
	modalAdd
	modalHelp
	modalDelete
)

func (m *Model) layout() (bounds, body image.Rectangle) {
	width, height := min(max(m.w-4, 1), 60), min(max(m.h, 1), 9)
	if m.w < 28 {
		width = max(m.w, 1)
	}
	if m.activeModal == modalHelp {
		height = min(max(m.h-2, 1), lipgloss.Height(shortcuts(max(width-6, 1)))+4)
	}
	left, top := max((m.w-width)/2, 0), max((m.h-height)/2, 0)
	bounds = image.Rect(left, top, left+width, top+height)
	body = bounds
	if width >= 4 && height >= 3 {
		paddingX, paddingY := min(2, (width-3)/2), min(1, (height-3)/2)
		body = image.Rect(left+1+paddingX, top+1+paddingY, left+width-1-paddingX, top+height-1-paddingY)
	}
	return bounds, body
}

func (m *Model) resizeModal() {
	if m.activeModal == modalNone {
		return
	}
	_, body := m.layout()
	switch m.activeModal {
	case modalAdd:
		m.editor.SetWidth(body.Dx())
		m.editor.SetHeight(body.Dy())
	case modalHelp:
		m.help.SetWidth(body.Dx())
		m.help.SetHeight(body.Dy())
		m.help.SetContent(shortcuts(body.Dx()))
	}
}

func (m *Model) closeModal() {
	m.navigation.Reset()
	m.activeModal = modalNone
	m.deleteTarget = tree.Row{}
	m.editor.Blur()
}

func (m *Model) updateModal(msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		key := msg.String()
		if key == "esc" || (key == "?" && m.activeModal == modalHelp) {
			m.closeModal()
			return nil
		}
		if m.activeModal == modalHelp {
			if len(key) == 1 && key[0] >= '0' && key[0] <= '9' {
				m.navigation.Reset()
				return nil
			}
			if motion, handled := m.navigation.Read(key); handled {
				switch motion.Action {
				case navigation.Move, navigation.Scroll, navigation.Page, navigation.HalfPage:
					m.help.SetYOffset(m.help.YOffset() + motion.Distance(m.help.Height(), m.help.TotalLineCount()))
				case navigation.Line:
					m.help.GotoTop()
				case navigation.Last:
					m.help.GotoBottom()
				}
				return nil
			}
		}
		if key == "enter" && m.activeModal == modalAdd {
			url := strings.TrimSpace(m.editor.Value())
			m.closeModal()
			if url == "" {
				return nil
			}
			return action(false, m.start, func() error {
				_, err := m.client.AddSubscription(m.watch, url)
				return err
			})
		}
		if key == "enter" && m.activeModal == modalDelete {
			target := m.deleteTarget
			m.closeModal()
			return action(false, m.start, func() error {
				if target.Kind == tree.Header {
					return m.client.RemoveSubscription(m.watch, target.Sub.SubscriptionID)
				}
				return m.client.RemoveNode(m.watch, target.Node.Ref())
			})
		}
	case tea.MouseMsg:
		m.navigation.Reset()
		_, body := m.layout()
		mouse := msg.Mouse()
		if m.activeModal != modalHelp || !image.Pt(mouse.X, mouse.Y).In(body) {
			return nil
		}
	}
	var cmd tea.Cmd
	switch m.activeModal {
	case modalAdd:
		m.editor, cmd = m.editor.Update(msg)
	case modalHelp:
		m.help, cmd = m.help.Update(msg)
	}
	return cmd
}

func (m *Model) modal(background string) string {
	bounds, body := m.layout()
	var title, content string
	switch m.activeModal {
	case modalAdd:
		title = "Add"
		editor := m.editor
		if editor.Value() == "" {
			editorStyles := editor.Styles()
			editorStyles.Focused.CursorLine = style.Gray
			editor.SetStyles(editorStyles)
		}
		content = editor.View()
	case modalHelp:
		title = "Shortcuts"
		content = m.help.View()
	case modalDelete:
		title = "Delete"
		name := m.deleteTarget.Sub.Name
		if m.deleteTarget.Kind == tree.Node {
			name = m.deleteTarget.Node.Name
		}
		const question = "Do you really want to delete "
		warning := ansi.Hardwrap("This action can't be undone.", body.Dx(), true)
		questionLines := max(body.Dy()-lipgloss.Height(warning), 1)
		name = ansi.Truncate(style.Sanitize(name, m.emoji()), max(body.Dx()*questionLines-len(question)-1, 0), "...")
		content = ansi.Hardwrap(question+style.Bold.Render(name)+"?", body.Dx(), true) + "\n" + warning
	}
	width, height := bounds.Dx(), bounds.Dy()
	if width < 4 || height < 3 {
		return style.Fit(style.Clip(content, width), height)
	}
	content = style.Fit(style.Clip(content, body.Dx()), body.Dy())
	border := lipgloss.RoundedBorder()
	if style.TTY {
		border = lipgloss.ASCIIBorder()
	}
	window := lipgloss.NewStyle().Border(border).BorderForeground(style.Blue.GetForeground()).
		Padding(body.Min.Y-bounds.Min.Y-1, body.Min.X-bounds.Min.X-1).
		Width(width).Height(height).Render(content)
	frame := strings.Split(window, "\n")
	titlePadding := min(2, width-4)
	title = ansi.Truncate(title, width-4-titlePadding, "")
	frame[0] = style.Blue.Render(border.TopLeft+strings.Repeat(border.Top, titlePadding)+" ") + style.Bold.Render(title) +
		style.Blue.Render(" "+strings.Repeat(border.Top, width-4-titlePadding-lipgloss.Width(title))+border.TopRight)
	if m.activeModal == modalAdd || m.activeModal == modalDelete {
		hint := style.KeyHint(style.Enter(), "submit")
		hintWidth := lipgloss.Width(hint)
		if width >= hintWidth+6 {
			frame[height-1] = style.Blue.Render(border.BottomLeft+strings.Repeat(border.Bottom, width-hintWidth-6)+" ") +
				hint + style.Blue.Render(" "+strings.Repeat(border.Bottom, 2)+border.BottomRight)
		}
	}
	canvas := lipgloss.NewCanvas(m.w, m.h).Compose(lipgloss.NewLayer(background))
	for row := range canvas.Height() {
		for column := range canvas.Width() {
			cellStyle := &canvas.CellAt(column, row).Style
			cellStyle.Attrs |= uv.AttrFaint
			if foreground := m.dimmedColors[cellStyle.Fg]; foreground != nil {
				cellStyle.Fg = foreground
				cellStyle.Attrs &^= uv.AttrFaint
			}
			if cellStyle.Bg != nil {
				cellStyle.Bg = m.dimmedColors[cellStyle.Bg]
			}
		}
	}
	lipgloss.NewLayer(strings.Join(frame, "\n")).Draw(canvas, bounds)
	return canvas.Render()
}
