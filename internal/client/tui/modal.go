package tui

import (
	"image"
	"strings"

	"charm.land/bubbles/v2/cursor"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"

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

func (model *Model) closeModal() tea.Cmd {
	if model.activeModal == modalHelp {
		model.activeModal, model.helpReturn = model.helpReturn, modalNone
		model.resizeModal()
		if model.activeModal == modalAdd || model.editing() {
			return cursor.Blink
		}
		return nil
	}
	model.activeModal = modalNone
	model.deleteTarget = tree.Row{}
	model.editor.Blur()
	if model.editing() {
		return cursor.Blink
	}
	return nil
}

func (model Model) updateModal(message tea.Msg) (tea.Model, tea.Cmd) {
	switch message := message.(type) {
	case tea.KeyPressMsg:
		if message.String() == "esc" || (message.String() == "?" && model.activeModal == modalHelp) {
			command := model.closeModal()
			return model, command
		}
		if message.String() == "enter" && model.activeModal == modalAdd {
			url := strings.TrimSpace(model.editor.Value())
			model.closeModal()
			if url == "" {
				return model, nil
			}
			return model, action(false, model.start(false), func() error {
				_, err := model.client.AddSubscription(model.watch, url)
				return err
			})
		}
		if message.String() == "enter" && model.activeModal == modalDelete {
			target := model.deleteTarget
			model.closeModal()
			return model, action(false, model.start(false), func() error {
				if target.Kind == tree.Header {
					return model.client.RemoveSubscription(model.watch, target.Sub.SubscriptionID)
				}
				return model.client.RemoveNode(model.watch, target.Node.Ref())
			})
		}
	case tea.MouseMsg:
		layout := model.modalLayout()
		mouse := message.Mouse()
		if model.activeModal != modalHelp || !image.Pt(mouse.X, mouse.Y).In(layout.body) {
			return model, nil
		}
	}
	var command tea.Cmd
	switch model.activeModal {
	case modalAdd:
		model.editor, command = model.editor.Update(message)
	case modalHelp:
		model.help, command = model.help.Update(message)
	}
	return model, command
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
	window := lipgloss.NewStyle().Border(border).BorderForeground(lipgloss.Color("4")).
		Padding(layout.body.Min.Y-layout.bounds.Min.Y-1, layout.body.Min.X-layout.bounds.Min.X-1).
		Width(width).Height(height).Render(content)
	edge := lipgloss.NewStyle().Foreground(lipgloss.Color("4"))
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
	subduedAccent := lipgloss.Color("#3d4a52")
	for row := range canvas.Height() {
		for column := range canvas.Width() {
			cell := canvas.CellAt(column, row)
			cell.Style.Attrs = (cell.Style.Attrs &^ uv.AttrBold) | uv.AttrFaint
			if cell.Style.Bg == lipgloss.Color("4") {
				cell.Style.Bg = subduedAccent
			}
			if cell.Style.Fg == lipgloss.Color("4") && (cell.Content == "▐" || cell.Content == "▌") {
				cell.Style.Fg = subduedAccent
				cell.Style.Attrs &^= uv.AttrFaint
			}
		}
	}
	lipgloss.NewLayer(strings.Join(frame, "\n")).Draw(canvas, layout.bounds)
	return canvas.Render()
}
