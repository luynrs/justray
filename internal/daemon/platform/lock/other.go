//go:build !unix && !windows

package lock

func File(string) (unlock func(), err error) { return func() {}, nil }
