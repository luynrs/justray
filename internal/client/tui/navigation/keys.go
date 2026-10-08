package navigation

import "math"

type Action uint8

const (
	Move Action = iota + 1
	Page
	HalfPage
	Scroll
	Line
	Last
)

type Motion struct {
	Action    Action
	Count     int
	Direction int
}

type Keys struct {
	count   int
	pending bool
}

func (keys *Keys) Reset() { *keys = Keys{} }

func (keys *Keys) Read(input string) (Motion, bool) {
	motion := Motion{Count: max(keys.count, 1), Direction: 1}
	if keys.pending {
		keys.Reset()
		if input == "g" {
			motion.Action = Line
			return motion, true
		}
		motion.Count = 1
	}
	if len(input) == 1 && input[0] >= '0' && input[0] <= '9' {
		digit := int(input[0] - '0')
		if keys.count > (math.MaxInt-digit)/10 {
			keys.count = math.MaxInt
		} else {
			keys.count = keys.count*10 + digit
		}
		return Motion{}, true
	}
	switch input {
	case "g":
		keys.pending = true
		return Motion{}, true
	case "up", "k":
		motion.Action, motion.Direction = Move, -1
	case "down", "j":
		motion.Action = Move
	case "pgup", "ctrl+b":
		motion.Action, motion.Direction = Page, -1
	case "pgdown", "ctrl+f":
		motion.Action = Page
	case "ctrl+u":
		motion.Action, motion.Direction, motion.Count = HalfPage, -1, keys.count
	case "ctrl+d":
		motion.Action, motion.Count = HalfPage, keys.count
	case "ctrl+y":
		motion.Action, motion.Direction = Scroll, -1
	case "ctrl+e":
		motion.Action = Scroll
	case "home":
		motion.Action, motion.Count = Line, 1
	case "end":
		motion.Action = Last
	case "G", "shift+g":
		motion.Action = Last
		if keys.count > 0 {
			motion.Action = Line
		}
	}
	keys.Reset()
	return motion, motion.Action != 0
}

func (motion Motion) Distance(height, limit int) int {
	step := 1
	if motion.Action == Page {
		step = max(height-1, 1)
	} else if motion.Action == HalfPage && motion.Count == 0 {
		step = max(height/2, 1)
	}
	if max(motion.Count, 1) > limit/step {
		return motion.Direction * limit
	}
	return motion.Direction * max(motion.Count, 1) * step
}
