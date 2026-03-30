package wizard

import "fmt"

const (
	reset  = "\033[0m"
	red    = "\033[31m"
	green  = "\033[32m"
	yellow = "\033[33m"
	cyan   = "\033[36m"
	bold   = "\033[1m"
)

func Red(s string) string    { return fmt.Sprintf("%s%s%s", red, s, reset) }
func Green(s string) string  { return fmt.Sprintf("%s%s%s", green, s, reset) }
func Yellow(s string) string { return fmt.Sprintf("%s%s%s", yellow, s, reset) }
func Bold(s string) string   { return fmt.Sprintf("%s%s%s", bold, s, reset) }

func Success(s string) { fmt.Printf("%s✓%s %s\n", green, reset, s) }
func Fail(s string)    { fmt.Printf("%s✗%s %s\n", red, reset, s) }
func Info(s string)    { fmt.Printf("%s→%s %s\n", cyan, reset, s) }
func Warn(s string)    { fmt.Printf("%s⚠%s %s\n", yellow, reset, s) }
func Header(s string) {
	sep := "═══════════════════════════"
	fmt.Printf("\n%s %s %s\n", sep, Bold(s), sep)
}
