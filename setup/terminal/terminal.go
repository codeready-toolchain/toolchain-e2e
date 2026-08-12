package terminal

import (
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/fatih/color"
	"github.com/manifoldco/promptui"
)

// Terminal a wrapper around a Cobra command, with extra methods
// to display messages.
type Terminal interface {
	InOrStdin() io.Reader
	OutOrStdout() io.Writer
	Debugf(msg string, args ...any)
	Info(msg string)
	Infof(msg string, args ...any)
	Error(err error, msg string)
	Errorf(err error, msg string, args ...any)
	Fatal(err error, msg string)
	Fatalf(err error, msg string, args ...any)
	PromptBoolf(msg string, args ...any) bool
	AddPreFatalExitHook(func())
}

// New returns a new terminal with the given funcs to
// access the `in` reader and `out` writer
func New(in func() io.Reader, out func() io.Writer, verbose bool) Terminal {
	return &DefaultTerminal{
		in:      in,
		out:     out,
		verbose: verbose,
	}
}

// InOrStdin returns an `io.Reader` to read the user's input
func (t *DefaultTerminal) InOrStdin() io.Reader {
	return t.in()
}

// OutOrStdout returns an `io.Writer` to write messages in the console
func (t *DefaultTerminal) OutOrStdout() io.Writer {
	return t.out()
}

// DefaultTerminal a wrapper around a Cobra command, with extra methods
// to display messages.
type DefaultTerminal struct {
	in             func() io.Reader
	out            func() io.Writer
	fatalExitHooks []func()
	verbose        bool
}

// Debugf prints a message (if verbose was enabled)
func (t *DefaultTerminal) Debugf(msg string, args ...any) {
	if !t.verbose {
		return
	}
	if msg == "" {
		fmt.Fprintln(t.OutOrStdout(), "")
		return
	}
	fmt.Fprintln(t.OutOrStdout(), fmt.Sprintf(msg, args...))
}

// Info displays a message with the default color
func (t *DefaultTerminal) Info(msg string) {
	if msg == "" {
		fmt.Fprintln(t.OutOrStdout(), "")
		return
	}
	fmt.Fprintln(t.OutOrStdout(), msg)
}

// Infof displays a message with the default color
func (t *DefaultTerminal) Infof(msg string, args ...any) {
	if msg == "" {
		fmt.Fprintln(t.OutOrStdout(), "")
		return
	}
	fmt.Fprintln(t.OutOrStdout(), fmt.Sprintf(msg, args...))
}

// Error prints a message with the red color
func (t *DefaultTerminal) Error(err error, msg string) {
	color.New(color.FgRed).Fprintln(t.OutOrStdout(), fmt.Sprintf("%s: %s", msg, err.Error())) // nolint:errcheck
}

// Errorf prints a message with the red color
func (t *DefaultTerminal) Errorf(err error, msg string, args ...any) {
	color.New(color.FgRed).Fprintln(t.OutOrStdout(), fmt.Sprintf("%s: %s", fmt.Sprintf(msg, args...), err.Error())) // nolint:errcheck
}

// Fatal prints a message with the red color and exits the program with a `1` return code
func (t *DefaultTerminal) Fatal(err error, msg string) {
	defer os.Exit(1)
	for _, hook := range t.fatalExitHooks {
		hook()
	}
	t.Error(err, msg)
}

// Fatalf prints a message with the red color and exits the program with a `1` return code
func (t *DefaultTerminal) Fatalf(err error, msg string, args ...any) {
	defer os.Exit(1)
	for _, hook := range t.fatalExitHooks {
		hook()
	}
	t.Errorf(err, msg, args...)
}

// PromptBoolf prints a message and waits for the user's boolean response
func (t *DefaultTerminal) PromptBoolf(msg string, args ...any) bool {
	fmt.Fprintln(t.OutOrStdout(), fmt.Sprintf(msg, args...))
	t.InOrStdin()

	prompt := promptui.Prompt{
		Label:     fmt.Sprintf(msg, args...),
		IsConfirm: true,
	}

	result, err := prompt.Run()

	if err != nil && !errors.Is(err, promptui.ErrAbort) {
		t.Errorf(err, "😳 Prompt failed")
		return false
	}
	return strings.ToLower(result) == "y"
}

func (t *DefaultTerminal) AddPreFatalExitHook(hook func()) {
	t.fatalExitHooks = append(t.fatalExitHooks, hook)
}
