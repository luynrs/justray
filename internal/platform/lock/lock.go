package lock

import "errors"

var ErrLocked = errors.New("daemon lock held")

func File(path string) (func(), error) {
	file, err := Open(path)
	if err != nil {
		return nil, err
	}
	return func() { Close(file) }, nil
}
