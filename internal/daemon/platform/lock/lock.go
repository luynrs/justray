package lock

import "errors"

var ErrLocked = errors.New("daemon lock held")
