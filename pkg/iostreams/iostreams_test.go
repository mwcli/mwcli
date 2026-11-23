package iostreams

import (
	"context"
	"errors"
	"io"
	"testing"
	"time"
)

func TestPromptContextReturnsOnCancel(t *testing.T) {
	ios, _, _, _ := Test()
	ios.SetTTY(true, true, true)
	input, inputWriter := io.Pipe()
	outputReader, output := io.Pipe()
	ios.In = input
	ios.ErrOut = output
	defer input.Close()
	defer outputReader.Close()

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, err := ios.PromptContext(ctx, "Choose: ")
		done <- err
	}()

	label := make([]byte, len("Choose: "))
	if _, err := io.ReadFull(outputReader, label); err != nil {
		t.Fatal(err)
	}
	cancel()

	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("PromptContext() error = %v, want context.Canceled", err)
		}
	case <-time.After(time.Second):
		t.Fatal("PromptContext() did not return after cancellation")
	}
	if err := inputWriter.Close(); err != nil {
		t.Fatal(err)
	}
}
