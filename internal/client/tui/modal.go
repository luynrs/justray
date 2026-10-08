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

func (model Model) modalLayout() modalLayout {
	width, height := min(max(model.w-4, 1), 60), min(max(model.h, 1), 9)
	if model.w < 28 {
		width = max(model.w, 1)
	}
	left, top := max((model.w-width)/2, 0), max((model.h-height)/2, 0)
	layout := modalLayout{bounds: image.Rect(left, top, left+width, top+height)}
	if width < 4 || height < 3 {
		layout.body = layout.bounds
		return layout
	}
	paddingX, paddingY := min(2, (width-3)/2), min(1, (height-3)/2)
	layout.body = image.Rect(left+1+paddingX, top+1+paddingY, left+width-1-paddingX, top+height-1-paddingY)
	return layout
}

func (model *Model) resizeModal() {
	layout := model.modalLayout()
	model.editor.SetWidth(layout.body.Dx())
	model.editor.SetHeight(layout.body.Dy())
	model.help.SetWidth(layout.body.Dx())
	model.help.SetHeight(layout.body.Dy())
	if model.activeModal == modalHelp {
		model.help.SetContent(model.shortcuts())
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

func (model Model) modalView(background string) string {
	layout := model.modalLayout()
	var title, content string
	switch model.activeModal {
	case modalAdd:
		title = "Add"
		if model.editor.Value() == "" {
			editorStyles := model.editor.Styles()
			editorStyles.Focused.CursorLine = style.Dim
			model.editor.SetStyles(editorStyles)
		}
		content = model.editor.View()
	case modalHelp:
		title = "Shortcuts"
		content = model.help.View()
	case modalDelete:
		title = "Delete"
		name := model.deleteTarget.Sub.Name
		if model.deleteTarget.Kind == tree.Node {
			name = model.deleteTarget.Node.Name
		}
		const question = "Do you really want to delete "
		warning := ansi.Hardwrap("This action can't be undone.", layout.body.Dx(), true)
		questionLines := max(layout.body.Dy()-lipgloss.Height(warning), 1)
		name = ansi.Truncate(style.Sanitize(name, model.emoji()), max(layout.body.Dx()*questionLines-len(question)-1, 0), "...")
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
	edge := lipgloss.NewStyle().Foreground(style.Title.GetForeground())
	frame := strings.Split(window, "\n")
	titlePadding := min(2, width-4)
	title = ansi.Truncate(title, width-4-titlePadding, "")
	frame[0] = edge.Render(border.TopLeft+strings.Repeat(border.Top, titlePadding)+" ") + style.Name.Render(title) +
		edge.Render(" "+strings.Repeat(border.Top, width-4-titlePadding-lipgloss.Width(title))+border.TopRight)
	if model.activeModal == modalAdd || model.activeModal == modalDelete {
		hint := style.Key.Render(style.Enter()) + style.Dim.Render(" submit")
		hintWidth := lipgloss.Width(hint)
		if width >= hintWidth+6 {
			frame[height-1] = edge.Render(border.BottomLeft+strings.Repeat(border.Bottom, width-hintWidth-6)+" ") +
				hint + edge.Render(" "+strings.Repeat(border.Bottom, 2)+border.BottomRight)
		}
	}
	canvas := lipgloss.NewCanvas(model.w, model.h).Compose(lipgloss.NewLayer(background))
	for row := range canvas.Height() {
		for column := range canvas.Width() {
			cell := canvas.CellAt(column, row)
			cell.Style.Attrs = (cell.Style.Attrs &^ uv.AttrBold) | uv.AttrFaint
			if cell.Style.Bg == style.Title.GetForeground() {
				cell.Style.Bg = style.Dim.GetForeground()
			}
			if cell.Style.Fg == style.Title.GetForeground() && (cell.Content == "▐" || cell.Content == "▌") {
				cell.Style.Fg = style.Dim.GetForeground()
				cell.Style.Attrs &^= uv.AttrFaint
			}
		}
	}
	lipgloss.NewLayer(strings.Join(frame, "\n")).Draw(canvas, layout.bounds)
	return canvas.Render()
}
