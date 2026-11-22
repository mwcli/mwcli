// Package iostreams wraps the standard streams and terminal detection.
package iostreams

import (
	"bytes"
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
	if !s.CanPrompt() {
		return "", ErrNoPrompt
	}
	fmt.Fprint(s.ErrOut, label)
	// One byte at a time, so that nothing after the line is read ahead and
	// lost to a later prompt.
	var line []byte
	b := make([]byte, 1)
	for {
		n, err := s.In.Read(b)
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
	if !s.CanPrompt() || s.inFd < 0 {
		return "", ErrNoPrompt
	}
	fmt.Fprint(s.ErrOut, label)
	b, err := term.ReadPassword(s.inFd)
	fmt.Fprintln(s.ErrOut)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(b)), nil
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
