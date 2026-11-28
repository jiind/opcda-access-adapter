package fixture

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// Signal creates a marker for the controller that owns the fixture registration.
func Signal(directory, name string) error {
	file, err := os.OpenFile(filepath.Join(directory, name), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return fmt.Errorf("signal fixture controller %s: %w", name, err)
	}
	return file.Close()
}

// WaitForSignal waits for the controller to complete a fixture state change.
func WaitForSignal(ctx context.Context, directory, name string) error {
	ctx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	path := filepath.Join(directory, name)
	for {
		if info, err := os.Stat(path); err == nil {
			if !info.Mode().IsRegular() {
				return fmt.Errorf("fixture controller signal %s is not a file", name)
			}
			return nil
		} else if !errors.Is(err, os.ErrNotExist) {
			return err
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("fixture controller did not signal %s: %w", name, ctx.Err())
		case <-time.After(100 * time.Millisecond):
		}
	}
}
