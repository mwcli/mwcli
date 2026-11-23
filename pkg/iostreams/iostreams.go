// Package iostreams wraps the standard streams and terminal detection.
package iostreams

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/mattn/go-isatty"
	"golang.org/x/term"
)

// IOStreams are the streams a command reads from and writes to.
type IOStreams struct {
	In     io.Reader
	Out    io.Writer
	ErrOut io.Writer

	stdinTTY  bool
	stdoutTTY bool
	stderrTTY bool
	inFd      int
}

// System returns streams bound to the process stdin, stdout and stderr.
func System() *IOStreams {
	tty := func(f *os.File) bool { return isatty.IsTerminal(f.Fd()) || isatty.IsCygwinTerminal(f.Fd()) }
	return &IOStreams{
		In: os.Stdin, Out: os.Stdout, ErrOut: os.Stderr,
		stdinTTY: tty(os.Stdin), stdoutTTY: tty(os.Stdout), stderrTTY: tty(os.Stderr),
		inFd: int(os.Stdin.Fd()),
	}
}

// Test returns in-memory streams for tests.
func Test() (*IOStreams, *bytes.Buffer, *bytes.Buffer, *bytes.Buffer) {
	in, out, errOut := &bytes.Buffer{}, &bytes.Buffer{}, &bytes.Buffer{}
	return &IOStreams{In: in, Out: out, ErrOut: errOut, inFd: -1}, in, out, errOut
}

func (s *IOStreams) IsStdinTTY() bool  { return s.stdinTTY }
func (s *IOStreams) IsStdoutTTY() bool { return s.stdoutTTY }

// SetTTY sets terminal detection, for tests.
func (s *IOStreams) SetTTY(stdin, stdout, stderr bool) {
	s.stdinTTY, s.stdoutTTY, s.stderrTTY = stdin, stdout, stderr
}

// CanPrompt reports whether interactive prompts are possible.
func (s *IOStreams) CanPrompt() bool { return s.stdinTTY && s.stderrTTY }

// ErrNoPrompt means a prompt was needed but stdin is not a terminal.
var ErrNoPrompt = errors.New("cannot prompt: stdin is not a terminal")

// Prompt reads one line after printing label to stderr.
func (s *IOStreams) Prompt(label string) (string, error) {
	return s.PromptContext(context.Background(), label)
}

// PromptContext reads one line after printing label, returning when ctx is canceled.
// Cancellation does not interrupt the underlying read, so callers must exit or
// close the input stream before reusing this IOStreams.
func (s *IOStreams) PromptContext(ctx context.Context, label string) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if !s.CanPrompt() {
		return "", ErrNoPrompt
	}
	fmt.Fprint(s.ErrOut, label)
	type result struct {
		line string
		err  error
	}
	read := make(chan result, 1)
	go func() {
		line, err := readLine(s.In)
		read <- result{line: line, err: err}
	}()
	select {
	case result := <-read:
		if err := ctx.Err(); err != nil {
			fmt.Fprintln(s.ErrOut)
			return "", err
		}
		return result.line, result.err
	case <-ctx.Done():
		fmt.Fprintln(s.ErrOut)
		return "", ctx.Err()
	}
}

func readLine(input io.Reader) (string, error) {
	// One byte at a time, so that nothing after the line is read ahead and
	// lost to a later prompt.
	var line []byte
	b := make([]byte, 1)
	for {
		n, err := input.Read(b)
		if n == 1 {
			if b[0] == '\n' {
				break
			}
			line = append(line, b[0])
		}
		if errors.Is(err, io.EOF) && len(line) > 0 {
			break
		}
		if err != nil {
			return "", err
		}
	}
	return strings.TrimSpace(string(line)), nil
}

// PromptSecret reads one line without echo after printing label to stderr.
func (s *IOStreams) PromptSecret(label string) (string, error) {
	return s.PromptSecretContext(context.Background(), label)
}

// PromptSecretContext reads a secret without echo and returns when ctx is canceled.
// It restores terminal state before returning, but the underlying terminal read
// may remain blocked; callers must exit before reusing this terminal after cancel.
func (s *IOStreams) PromptSecretContext(ctx context.Context, label string) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if !s.CanPrompt() || s.inFd < 0 {
		return "", ErrNoPrompt
	}
	return promptSecretContext(ctx, s.ErrOut, label, s.inFd, term.GetState, term.ReadPassword, term.Restore)
}

func promptSecretContext(
	ctx context.Context,
	out io.Writer,
	label string,
	fd int,
	getState func(int) (*term.State, error),
	readPassword func(int) ([]byte, error),
	restore func(int, *term.State) error,
) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	state, err := getState(fd)
	if err != nil {
		return "", err
	}
	fmt.Fprint(out, label)
	type result struct {
		secret []byte
		err    error
	}
	read := make(chan result, 1)
	go func() {
		secret, err := readPassword(fd)
		read <- result{secret: secret, err: err}
	}()
	canceled := func() (string, error) {
		if err := restore(fd, state); err != nil {
			fmt.Fprintf(out, "\nwarning: could not restore terminal: %v", err)
		}
		fmt.Fprintln(out)
		return "", ctx.Err()
	}
	select {
	case result := <-read:
		if ctx.Err() != nil {
			return canceled()
		}
		fmt.Fprintln(out)
		if result.err != nil {
			return "", result.err
		}
		return strings.TrimSpace(string(result.secret)), nil
	case <-ctx.Done():
		return canceled()
	}
}

func (s *IOStreams) color(code, text string) string {
	if !s.stdoutTTY {
		return text
	}
	return "\x1b[" + code + "m" + text + "\x1b[0m"
}

func (s *IOStreams) SuccessIcon() string   { return s.color("32", "✓") }
func (s *IOStreams) FailureIcon() string   { return s.color("31", "X") }
func (s *IOStreams) WarningIcon() string   { return s.color("33", "!") }
func (s *IOStreams) Bold(t string) string  { return s.color("1", t) }
func (s *IOStreams) Muted(t string) string { return s.color("90", t) }
