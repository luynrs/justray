package autostart

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

func Current(ctx context.Context) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	enabled, err := Enabled(ctx)
	if err != nil {
		return "", fmt.Errorf("read autostart: %w", err)
	}
	if enabled {
		return "on", nil
	}
	return "off", nil
}

func Set(ctx context.Context, enabled bool) error {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	var err error
	if enabled {
		err = Enable(ctx)
	} else {
		err = Disable(ctx)
	}
	if err != nil {
		return err
	}
	actual, err := Enabled(ctx)
	if err == nil && actual != enabled {
		err = errors.New("autostart state did not change")
	}
	return err
}

func run(ctx context.Context, command *exec.Cmd) ([]byte, error) {
	command.WaitDelay = time.Second
	output, err := command.CombinedOutput()
	if ctx.Err() != nil {
		return output, ctx.Err()
	}
	if err != nil {
		err = fmt.Errorf("%s: %w: %s", command.Args[0], err, strings.TrimSpace(string(output)))
	}
	return output, err
}
