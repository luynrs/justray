package logger

import (
	"io"
	"log"
	"os"
)

func New(out io.Writer, source string) *log.Logger {
	return log.New(out, source+": ", log.LstdFlags)
}

func Open(path string) (*os.File, error) {
	return os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
}
