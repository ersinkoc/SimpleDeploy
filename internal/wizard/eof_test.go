package wizard

import (
	"bufio"
	"os"
	"strings"
	"testing"
	"time"
)

// runWithDeadline fails the test if fn has not returned within d. These
// functions loop until they get valid input, so a regression here does not
// produce a wrong value — it hangs the process forever, which a plain
// assertion would never catch.
func runWithDeadline(t *testing.T, d time.Duration, fn func()) {
	t.Helper()
	done := make(chan struct{})
	go func() {
		defer close(done)
		fn()
	}()
	select {
	case <-done:
	case <-time.After(d):
		t.Fatal("call did not return; the EOF retry loop is spinning forever")
	}
}

// TestAskRequired_EOFDoesNotHang is the regression test for a CLI that could
// never exit. With stdin closed — piped input, a non-interactive shell,
// `simpledeploy deploy < /dev/null` — scanner.Scan() returns false forever, so
// the old loop printed "This field is required." without end.
func TestAskRequired_EOFDoesNotHang(t *testing.T) {
	restore := quiet(t)
	defer restore()

	scanner = bufio.NewScanner(strings.NewReader(""))
	defer func() { scanner = bufio.NewScanner(os.Stdin) }()

	var got string
	runWithDeadline(t, 2*time.Second, func() { got = AskRequired("Name") })

	if got != "" {
		t.Errorf("AskRequired() at EOF = %q, want \"\" so the caller's validation reports a real error", got)
	}
}

// TestAskRequired_EOFAfterEmptyLines covers the mixed case: real (empty) input
// first, then the stream ends.
func TestAskRequired_EOFAfterEmptyLines(t *testing.T) {
	restore := quiet(t)
	defer restore()

	scanner = bufio.NewScanner(strings.NewReader("\n\n"))
	defer func() { scanner = bufio.NewScanner(os.Stdin) }()

	runWithDeadline(t, 2*time.Second, func() { AskRequired("Name") })
}

// TestChoose_EOFDoesNotHang mirrors the above for the menu prompt.
func TestChoose_EOFDoesNotHang(t *testing.T) {
	restore := quiet(t)
	defer restore()

	scanner = bufio.NewScanner(strings.NewReader(""))
	defer func() { scanner = bufio.NewScanner(os.Stdin) }()

	var got int
	runWithDeadline(t, 2*time.Second, func() { got = Choose("Pick:", []string{"A", "B", "C"}, 2) })

	if got != 2 {
		t.Errorf("Choose() at EOF = %d, want the supplied default 2", got)
	}
}

// TestChoose_EOFNoDefault: with no usable default, fall back to the first
// option rather than looping.
func TestChoose_EOFNoDefault(t *testing.T) {
	restore := quiet(t)
	defer restore()

	scanner = bufio.NewScanner(strings.NewReader(""))
	defer func() { scanner = bufio.NewScanner(os.Stdin) }()

	var got int
	runWithDeadline(t, 2*time.Second, func() { got = Choose("Pick:", []string{"A", "B"}, 0) })

	if got != 1 {
		t.Errorf("Choose() at EOF with no default = %d, want 1", got)
	}
}

// TestChoose_InvalidInputThenEOF: invalid entries are retried as before, but
// the stream ending still terminates the loop.
func TestChoose_InvalidInputThenEOF(t *testing.T) {
	restore := quiet(t)
	defer restore()

	scanner = bufio.NewScanner(strings.NewReader("99\nnope\n"))
	defer func() { scanner = bufio.NewScanner(os.Stdin) }()

	var got int
	runWithDeadline(t, 2*time.Second, func() { got = Choose("Pick:", []string{"A", "B"}, 1) })

	if got != 1 {
		t.Errorf("Choose() = %d, want the default 1 after input was exhausted", got)
	}
}

func TestChoose_NoOptions(t *testing.T) {
	restore := quiet(t)
	defer restore()

	scanner = bufio.NewScanner(strings.NewReader(""))
	defer func() { scanner = bufio.NewScanner(os.Stdin) }()

	var got int
	runWithDeadline(t, 2*time.Second, func() { got = Choose("Pick:", nil, 0) })

	if got != 0 {
		t.Errorf("Choose() with no options = %d, want 0", got)
	}
}

// TestAskLine_DistinguishesEmptyFromEOF pins the invariant every retry loop
// depends on: pressing Enter and reaching EOF must not look the same.
func TestAskLine_DistinguishesEmptyFromEOF(t *testing.T) {
	restore := quiet(t)
	defer restore()
	defer func() { scanner = bufio.NewScanner(os.Stdin) }()

	scanner = bufio.NewScanner(strings.NewReader("\n"))
	if val, ok := askLine("P", ""); !ok || val != "" {
		t.Errorf("empty line = (%q, %v), want (\"\", true)", val, ok)
	}

	scanner = bufio.NewScanner(strings.NewReader(""))
	if val, ok := askLine("P", ""); ok || val != "" {
		t.Errorf("EOF = (%q, %v), want (\"\", false)", val, ok)
	}
}
