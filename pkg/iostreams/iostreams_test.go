package iostreams

import (
	"bytes"
	"context"
	"errors"
	"io"
	"sync"
	"testing"
	"time"

	"golang.org/x/term"
)

func TestPromptContextReturnsOnCancel(t *testing.T) {
	ios, _, _, _ := Test()
	ios.SetTTY(true, true, true)
	input, inputWriter := io.Pipe()
	outputReader, output := io.Pipe()
	ios.In = input
	ios.ErrOut = output
	defer input.Close()
	defer inputWriter.Close()
	defer outputReader.Close()
	defer output.Close()

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
	newline := make([]byte, 1)
	if _, err := io.ReadFull(outputReader, newline); err != nil {
		t.Fatal(err)
	}
	if string(newline) != "\n" {
		t.Fatalf("PromptContext() cancellation output = %q, want newline", newline)
	}

	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("PromptContext() error = %v, want context.Canceled", err)
		}
	case <-time.After(time.Second):
		t.Fatal("PromptContext() did not return after cancellation")
	}
}

func TestPromptContextWithCanceledContextDoesNotPrompt(t *testing.T) {
	ios, _, _, errOut := Test()
	ios.SetTTY(true, true, true)
	input := bytes.NewBufferString("answer\n")
	ios.In = input
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := ios.PromptContext(ctx, "Choose: ")
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("PromptContext() error = %v, want context.Canceled", err)
	}
	if errOut.Len() != 0 || input.Len() != len("answer\n") {
		t.Fatalf("PromptContext() wrote output or read input before returning cancellation")
	}
}

func TestPromptSecretContextWithCanceledContextDoesNotPrompt(t *testing.T) {
	ios, _, _, errOut := Test()
	ios.SetTTY(true, true, true)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := ios.PromptSecretContext(ctx, "Secret: ")
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("PromptSecretContext() error = %v, want context.Canceled", err)
	}
	if errOut.Len() != 0 {
		t.Fatal("PromptSecretContext() printed a label before returning cancellation")
	}
}

func TestPromptSecretContextRestoresTerminalOnCancel(t *testing.T) {
	state := &term.State{}
	started, unblockRead, readDone := make(chan struct{}), make(chan struct{}), make(chan struct{})
	var unblockOnce sync.Once
	unblock := func() { unblockOnce.Do(func() { close(unblockRead) }) }
	defer unblock()
	restored := make(chan *term.State, 1)
	var out bytes.Buffer
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, err := promptSecretContext(ctx, &out, "Secret: ", 7,
			func(fd int) (*term.State, error) { return state, nil },
			func(fd int) ([]byte, error) {
				close(started)
				<-unblockRead
				close(readDone)
				return nil, nil
			},
			func(fd int, got *term.State) error {
				restored <- got
				return nil
			},
		)
		done <- err
	}()

	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("password reader did not start")
	}
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("PromptSecretContext() error = %v, want context.Canceled", err)
		}
	case <-time.After(time.Second):
		t.Fatal("PromptSecretContext() did not return after cancellation")
	}
	select {
	case got := <-restored:
		if got != state {
			t.Fatal("PromptSecretContext() restored a different terminal state")
		}
	case <-time.After(time.Second):
		t.Fatal("PromptSecretContext() did not restore terminal state")
	}
	if got := out.String(); got != "Secret: \n" {
		t.Fatalf("PromptSecretContext() cancellation output = %q, want label and newline", got)
	}
	unblock()
	select {
	case <-readDone:
	case <-time.After(time.Second):
		t.Fatal("password reader did not stop after input was unblocked")
	}
}
