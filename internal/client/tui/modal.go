package tui

import (
	"image"
	"strings"

	"charm.land/bubbles/v2/cursor"
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

type modalLayout struct {
	bounds image.Rectangle
	body   image.Rectangle
}

func (m Model) modalLayout() modalLayout {
	width, height := min(max(m.w-4, 1), 60), min(max(m.h, 1), 9)
	if m.w < 28 {
		width = max(m.w, 1)
	}
	if m.activeModal == modalHelp {
		height = min(max(m.h-2, 1), lipgloss.Height(m.shortcuts(max(width-6, 1)))+4)
	}
	left, top := max((m.w-width)/2, 0), max((m.h-height)/2, 0)
	layout := modalLayout{bounds: image.Rect(left, top, left+width, top+height)}
	if width < 4 || height < 3 {
		layout.body = layout.bounds
		return layout
	}
	paddingX, paddingY := min(2, (width-3)/2), min(1, (height-3)/2)
	layout.body = image.Rect(left+1+paddingX, top+1+paddingY, left+width-1-paddingX, top+height-1-paddingY)
	return layout
}

func (m *Model) resizeModal() {
	layout := m.modalLayout()
	m.editor.SetWidth(layout.body.Dx())
	m.editor.SetHeight(layout.body.Dy())
	m.help.SetWidth(layout.body.Dx())
	m.help.SetHeight(layout.body.Dy())
	if m.activeModal == modalHelp {
		m.help.SetContent(m.shortcuts(layout.body.Dx()))
	}
}

func (m *Model) closeModal() tea.Cmd {
	m.navigation.Reset()
	m.activeModal = modalNone
	m.deleteTarget = tree.Row{}
	m.editor.Blur()
	if m.editing() {
		return cursor.Blink
	}
	return nil
}

func (m Model) updateModal(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		if msg.String() == "esc" || (msg.String() == "?" && m.activeModal == modalHelp) {
			cmd := m.closeModal()
			return m, cmd
		}
		if m.activeModal == modalHelp {
			if key := msg.String(); len(key) == 1 && key[0] >= '0' && key[0] <= '9' {
				m.navigation.Reset()
				return m, nil
			}
			if motion, handled := m.navigation.Read(msg.String()); handled {
				switch motion.Action {
				case navigation.Move, navigation.Scroll, navigation.Page, navigation.HalfPage:
					m.help.SetYOffset(m.help.YOffset() + motion.Distance(m.help.Height(), m.help.TotalLineCount()))
				case navigation.Line:
					m.help.GotoTop()
				case navigation.Last:
					m.help.GotoBottom()
				}
				return m, nil
			}
		}
		if msg.String() == "enter" && m.activeModal == modalAdd {
			url := strings.TrimSpace(m.editor.Value())
			m.closeModal()
			if url == "" {
				return m, nil
			}
			return m, action(false, m.start(false), func() error {
				_, err := m.client.AddSubscription(m.watch, url)
				return err
			})
		}
		if msg.String() == "enter" && m.activeModal == modalDelete {
			target := m.deleteTarget
			m.closeModal()
			return m, action(false, m.start(false), func() error {
				if target.Kind == tree.Header {
					return m.client.RemoveSubscription(m.watch, target.Sub.SubscriptionID)
				}
				return m.client.RemoveNode(m.watch, target.Node.Ref())
			})
		}
	case tea.MouseMsg:
		m.navigation.Reset()
		layout := m.modalLayout()
		mouse := msg.Mouse()
		if m.activeModal != modalHelp || !image.Pt(mouse.X, mouse.Y).In(layout.body) {
			return m, nil
		}
	}
	var cmd tea.Cmd
	switch m.activeModal {
	case modalAdd:
		m.editor, cmd = m.editor.Update(msg)
	case modalHelp:
		m.help, cmd = m.help.Update(msg)
	}
	return m, cmd
}

func (m Model) modalView(background string) string {
	layout := m.modalLayout()
	var title, content string
	switch m.activeModal {
	case modalAdd:
		title = "Add"
		if m.editor.Value() == "" {
			editorStyles := m.editor.Styles()
			editorStyles.Focused.CursorLine = style.Dim
			m.editor.SetStyles(editorStyles)
		}
		content = m.editor.View()
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
		warning := ansi.Hardwrap("This action can't be undone.", layout.body.Dx(), true)
		questionLines := max(layout.body.Dy()-lipgloss.Height(warning), 1)
		name = ansi.Truncate(style.Sanitize(name, m.emoji()), max(layout.body.Dx()*questionLines-len(question)-1, 0), "...")
		content = ansi.Hardwrap(question+style.Name.Render(name)+"?", layout.body.Dx(), true) + "\n" + warning
	}
	width, height := layout.bounds.Dx(), layout.bounds.Dy()
	if width < 4 || height < 3 {
		return style.Fit(style.Clip(content, width), height)
	}
	content = style.Fit(style.Clip(content, layout.body.Dx()), layout.body.Dy())
	border := lipgloss.RoundedBorder()
	if style.TTY {
		border = lipgloss.ASCIIBorder()
	}
	window := lipgloss.NewStyle().Border(border).BorderForeground(style.Title.GetForeground()).
		Padding(layout.body.Min.Y-layout.bounds.Min.Y-1, layout.body.Min.X-layout.bounds.Min.X-1).
		Width(width).Height(height).Render(content)
	edge := style.Title.Bold(false)
	frame := strings.Split(window, "\n")
	titlePadding := min(2, width-4)
	title = ansi.Truncate(title, width-4-titlePadding, "")
	frame[0] = edge.Render(border.TopLeft+strings.Repeat(border.Top, titlePadding)+" ") + style.Name.Render(title) +
		edge.Render(" "+strings.Repeat(border.Top, width-4-titlePadding-lipgloss.Width(title))+border.TopRight)
	if m.activeModal == modalAdd || m.activeModal == modalDelete {
		hint := style.KeyHint(style.Enter(), "submit")
		hintWidth := lipgloss.Width(hint)
		if width >= hintWidth+6 {
			frame[height-1] = edge.Render(border.BottomLeft+strings.Repeat(border.Bottom, width-hintWidth-6)+" ") +
				hint + edge.Render(" "+strings.Repeat(border.Bottom, 2)+border.BottomRight)
		}
	}
	canvas := lipgloss.NewCanvas(m.w, m.h).Compose(lipgloss.NewLayer(background))
	for row := range canvas.Height() {
		for column := range canvas.Width() {
			cell := canvas.CellAt(column, row)
			if cell.Style.Fg == style.Title.GetForeground() {
				cell.Style.Fg = style.MutedTitle.GetForeground()
			} else {
				cell.Style.Attrs |= uv.AttrFaint
			}
			if cell.Style.Bg == style.Title.GetForeground() {
				cell.Style.Bg = style.MutedTitle.GetForeground()
			}
		}
	}
	lipgloss.NewLayer(strings.Join(frame, "\n")).Draw(canvas, layout.bounds)
	return canvas.Render()
}
