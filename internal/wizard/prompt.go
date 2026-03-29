package wizard

import (
	"bufio"
	"fmt"
	"os"
	"strconv"
	"strings"
)

var scanner = bufio.NewScanner(os.Stdin)

func Ask(prompt, defaultVal string) string {
	if defaultVal != "" {
		fmt.Printf("%s [%s]: ", prompt, defaultVal)
	} else {
		fmt.Printf("%s: ", prompt)
	}
	if scanner.Scan() {
		val := strings.TrimSpace(scanner.Text())
		if val == "" {
			return defaultVal
		}
		return val
	}
	return defaultVal
}

func AskRequired(prompt string) string {
	for {
		fmt.Printf("%s: ", prompt)
		if scanner.Scan() {
			val := strings.TrimSpace(scanner.Text())
			if val != "" {
				return val
			}
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

func Choose(prompt string, options []string, defaultIdx int) int {
	for i, opt := range options {
		fmt.Printf("  [%d] %s\n", i+1, opt)
	}

	for {
		defStr := ""
		if defaultIdx > 0 {
			defStr = strconv.Itoa(defaultIdx)
		}
		input := Ask(prompt, defStr)
		if input == "" && defaultIdx > 0 {
			return defaultIdx
		}
		idx, err := strconv.Atoi(input)
		if err == nil && idx >= 1 && idx <= len(options) {
			return idx
		}
		Fail(fmt.Sprintf("Please enter a number between 1 and %d", len(options)))
	}
}

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
			if idx, err := strconv.Atoi(s); err == nil && idx >= 1 && idx <= len(options) {
				result = append(result, idx)
			}
		}
		return result
	}

	idx, err := strconv.Atoi(input)
	if err == nil && idx >= 1 && idx <= len(options) {
		return []int{idx}
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
