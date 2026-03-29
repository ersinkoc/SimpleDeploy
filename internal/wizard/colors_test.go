package wizard

import (
	"strings"
	"testing"
)

func TestColorFunctions(t *testing.T) {
	tests := []struct {
		name string
		fn   func(string) string
	}{
		{"Red", Red},
		{"Green", Green},
		{"Yellow", Yellow},
		{"Blue", Blue},
		{"Cyan", Cyan},
		{"Bold", Bold},
		{"Magenta", Magenta},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.fn("test")
			if !strings.Contains(result, "test") {
				t.Errorf("%s(\"test\") does not contain 'test'", tt.name)
			}
			if !strings.HasPrefix(result, "\033[") {
				t.Errorf("%s(\"test\") should start with ANSI escape", tt.name)
			}
			if !strings.HasSuffix(result, "\033[0m") {
				t.Errorf("%s(\"test\") should end with reset code", tt.name)
			}
		})
	}
}

func TestRed(t *testing.T) {
	got := Red("error")
	if got == "error" {
		t.Error("Red should wrap with ANSI codes")
	}
	if !strings.Contains(got, "error") {
		t.Error("Red should contain original text")
	}
}

func TestGreen(t *testing.T) {
	got := Green("ok")
	if !strings.Contains(got, "ok") {
		t.Error("Green should contain original text")
	}
}

func TestBold(t *testing.T) {
	got := Bold("title")
	if !strings.Contains(got, "title") {
		t.Error("Bold should contain original text")
	}
}

func TestSuccessPrints(t *testing.T) {
	// Just verify no panic
	Success("test message")
}

func TestFailPrints(t *testing.T) {
	Fail("test error")
}

func TestInfoPrints(t *testing.T) {
	Info("test info")
}

func TestWarnPrints(t *testing.T) {
	Warn("test warning")
}

func TestHeaderPrints(t *testing.T) {
	Header("Test Header")
}

func TestColorEmptyString(t *testing.T) {
	got := Red("")
	if !strings.Contains(got, "\033[0m") {
		t.Error("Should still wrap empty string with reset")
	}
}

func TestColorSpecialChars(t *testing.T) {
	got := Green("hello\nworld\t!")
	if !strings.Contains(got, "hello\nworld\t!") {
		t.Error("Should handle special chars")
	}
}
