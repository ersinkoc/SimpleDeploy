package wizard

import (
	"bufio"
	"fmt"
	"os"
	"strconv"
	"strings"
)

var scanner = bufio.NewScanner(os.Stdin)

// SetScannerForTesting replaces the global scanner with the given one.
// This is intended ONLY for use in tests to simulate user input.
func SetScannerForTesting(s *bufio.Scanner) {
	scanner = s
}

// askLine prints the prompt and reads one line. The second return value is
// false when the input stream is exhausted (EOF) — as opposed to the user
// simply pressing Enter, which returns ("", true). Callers that loop MUST
// check it: without the distinction, a closed stdin (piped input, a
// non-interactive shell, `simpledeploy deploy < /dev/null`) turns every
// retry loop below into a spin that prints forever and never terminates.
func askLine(prompt, defaultVal string) (string, bool) {
	if defaultVal != "" {
		fmt.Printf("%s [%s]: ", prompt, defaultVal)
	} else {
		fmt.Printf("%s: ", prompt)
	}
	if !scanner.Scan() {
		return "", false
	}
	return strings.TrimSpace(scanner.Text()), true
}

func Ask(prompt, defaultVal string) string {
	val, ok := askLine(prompt, defaultVal)
	if !ok || val == "" {
		return defaultVal
	}
	return val
}

// AskRequired loops until the user supplies a non-empty value. On EOF it
// gives up and returns "" rather than looping forever; callers validate the
// result (state.Validate*, or an explicit emptiness check) and surface a
// real error, which is the correct outcome for a non-interactive run.
func AskRequired(prompt string) string {
	for {
		val, ok := askLine(prompt, "")
		if !ok {
			Fail("Input stream closed; this field is required.")
			return ""
		}
		if val != "" {
			return val
		}
		Fail("This field is required.")
	}
}

func Confirm(prompt string, defaultYes bool) bool {
	if defaultYes {
		fmt.Printf("%s [Y/n]: ", prompt)
	} else {
		fmt.Printf("%s [y/N]: ", prompt)
	}
	if scanner.Scan() {
		val := strings.ToLower(strings.TrimSpace(scanner.Text()))
		if val == "" {
			return defaultYes
		}
		return val == "y" || val == "yes"
	}
	return defaultYes
}

// Choose loops until a valid 1-based option index is entered. Like
// AskRequired it terminates on EOF instead of spinning: it falls back to
// defaultIdx, or to option 1 when no default was supplied.
func Choose(prompt string, options []string, defaultIdx int) int {
	if len(options) == 0 {
		return 0
	}
	for i, opt := range options {
		fmt.Printf("  [%d] %s\n", i+1, opt)
	}

	for {
		defStr := ""
		if defaultIdx > 0 {
			defStr = strconv.Itoa(defaultIdx)
		}
		input, ok := askLine(prompt, defStr)
		if !ok {
			Fail("Input stream closed; using default selection.")
			if defaultIdx >= 1 && defaultIdx <= len(options) {
				return defaultIdx
			}
			return 1
		}
		if input == "" {
			input = defStr
		}
		idx, err := strconv.Atoi(input)
		if err == nil && idx >= 1 && idx <= len(options) {
			return idx
		}
		Fail(fmt.Sprintf("Please enter a number between 1 and %d", len(options)))
	}
}

// MultiChoose asks for one selection, or several via the "0" sub-prompt.
//
// Invalid input is reported rather than swallowed. Previously any unparseable
// or out-of-range answer just returned nil, and since the deploy wizard reads
// nil as "no databases selected", typing `postgres` instead of `2` at the
// database prompt silently produced an app with no database and no
// DATABASE_URL — the only clue being a missing line in the summary.
func MultiChoose(prompt string, options []string) []int {
	for i, opt := range options {
		fmt.Printf("  [%d] %s\n", i+1, opt)
	}
	fmt.Println("  [0] Select multiple (comma-separated)")

	input := Ask(prompt, "")
	if input == "0" {
		listStr := Ask("Enter selections (comma-separated)", "")
		var result []int
		for _, s := range strings.Split(listStr, ",") {
			s = strings.TrimSpace(s)
			if s == "" {
				continue
			}
			idx, err := strconv.Atoi(s)
			if err != nil || idx < 1 || idx > len(options) {
				Fail(fmt.Sprintf("Ignoring invalid selection %q (expected 1-%d)", s, len(options)))
				continue
			}
			result = append(result, idx)
		}
		if len(result) == 0 {
			Warn("No valid selection made.")
		}
		return result
	}

	idx, err := strconv.Atoi(input)
	if err == nil && idx >= 1 && idx <= len(options) {
		return []int{idx}
	}
	if input != "" {
		Fail(fmt.Sprintf("Ignoring invalid selection %q (expected a number 1-%d, or 0 to pick several)", input, len(options)))
	}
	return nil
}

func AskMultiple(prompt string) []string {
	fmt.Println(prompt + " (leave empty to finish)")
	var results []string
	for {
		fmt.Print("> ")
		if scanner.Scan() {
			val := strings.TrimSpace(scanner.Text())
			if val == "" {
				break
			}
			results = append(results, val)
		} else {
			break
		}
	}
	return results
}
