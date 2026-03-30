package wizard

import (
	"bufio"
	"os"
	"strings"
	"testing"
)

func quiet(t *testing.T) func() {
	t.Helper()
	old := os.Stdout
	f, err := os.OpenFile(os.DevNull, os.O_WRONLY, 0)
	if err != nil {
		t.Skip("Can't open devnull")
	}
	os.Stdout = f
	return func() {
		os.Stdout = old
		f.Close()
	}
}

func TestAsk_WithInput(t *testing.T) {
	restore := quiet(t)
	defer restore()

	scanner = bufio.NewScanner(strings.NewReader("hello\n"))
	defer func() { scanner = bufio.NewScanner(os.Stdin) }()

	result := Ask("Name", "")
	if result != "hello" {
		t.Errorf("Ask() = %q, want 'hello'", result)
	}
}

func TestAsk_UsesDefault(t *testing.T) {
	restore := quiet(t)
	defer restore()

	scanner = bufio.NewScanner(strings.NewReader("\n"))
	defer func() { scanner = bufio.NewScanner(os.Stdin) }()

	result := Ask("Name", "default_val")
	if result != "default_val" {
		t.Errorf("Ask() with empty input = %q, want 'default_val'", result)
	}
}

func TestAsk_EmptyDefault(t *testing.T) {
	restore := quiet(t)
	defer restore()

	scanner = bufio.NewScanner(strings.NewReader("\n"))
	defer func() { scanner = bufio.NewScanner(os.Stdin) }()

	result := Ask("Name", "")
	if result != "" {
		t.Errorf("Ask() with empty input and no default = %q, want ''", result)
	}
}

func TestAskOverrideInput(t *testing.T) {
	restore := quiet(t)
	defer restore()

	scanner = bufio.NewScanner(strings.NewReader("custom\n"))
	defer func() { scanner = bufio.NewScanner(os.Stdin) }()

	result := Ask("Name", "default_val")
	if result != "custom" {
		t.Errorf("Ask() = %q, want 'custom'", result)
	}
}

func TestAskRequired_ValidInput(t *testing.T) {
	restore := quiet(t)
	defer restore()

	scanner = bufio.NewScanner(strings.NewReader("required_value\n"))
	defer func() { scanner = bufio.NewScanner(os.Stdin) }()

	result := AskRequired("Name")
	if result != "required_value" {
		t.Errorf("AskRequired() = %q, want 'required_value'", result)
	}
}

func TestAskRequired_RetriesOnEmpty(t *testing.T) {
	restore := quiet(t)
	defer restore()

	// First input is empty (just newline), second is valid
	scanner = bufio.NewScanner(strings.NewReader("\nvalid_input\n"))
	defer func() { scanner = bufio.NewScanner(os.Stdin) }()

	result := AskRequired("Name")
	if result != "valid_input" {
		t.Errorf("AskRequired() = %q, want 'valid_input'", result)
	}
}

func TestConfirm_Yes(t *testing.T) {
	restore := quiet(t)
	defer restore()

	scanner = bufio.NewScanner(strings.NewReader("y\n"))
	defer func() { scanner = bufio.NewScanner(os.Stdin) }()

	result := Confirm("Continue?", true)
	if !result {
		t.Error("Confirm('y') should return true")
	}
}

func TestConfirm_No(t *testing.T) {
	restore := quiet(t)
	defer restore()

	scanner = bufio.NewScanner(strings.NewReader("n\n"))
	defer func() { scanner = bufio.NewScanner(os.Stdin) }()

	result := Confirm("Continue?", true)
	if result {
		t.Error("Confirm('n') should return false")
	}
}

func TestConfirm_DefaultYes(t *testing.T) {
	restore := quiet(t)
	defer restore()

	scanner = bufio.NewScanner(strings.NewReader("\n"))
	defer func() { scanner = bufio.NewScanner(os.Stdin) }()

	result := Confirm("Continue?", true)
	if !result {
		t.Error("Confirm with defaultYes=true and empty input should return true")
	}
}

func TestConfirm_DefaultNo(t *testing.T) {
	restore := quiet(t)
	defer restore()

	scanner = bufio.NewScanner(strings.NewReader("\n"))
	defer func() { scanner = bufio.NewScanner(os.Stdin) }()

	result := Confirm("Continue?", false)
	if result {
		t.Error("Confirm with defaultYes=false and empty input should return false")
	}
}

func TestConfirm_FullWord(t *testing.T) {
	restore := quiet(t)
	defer restore()

	scanner = bufio.NewScanner(strings.NewReader("yes\n"))
	defer func() { scanner = bufio.NewScanner(os.Stdin) }()

	result := Confirm("Continue?", false)
	if !result {
		t.Error("Confirm('yes') should return true")
	}
}

func TestConfirm_UpperCase(t *testing.T) {
	restore := quiet(t)
	defer restore()

	scanner = bufio.NewScanner(strings.NewReader("Y\n"))
	defer func() { scanner = bufio.NewScanner(os.Stdin) }()

	result := Confirm("Continue?", false)
	if !result {
		t.Error("Confirm('Y') should return true")
	}
}

func TestChoose_ValidSelection(t *testing.T) {
	restore := quiet(t)
	defer restore()

	scanner = bufio.NewScanner(strings.NewReader("2\n"))
	defer func() { scanner = bufio.NewScanner(os.Stdin) }()

	options := []string{"Option A", "Option B", "Option C"}
	result := Choose("Pick one:", options, 0)
	if result != 2 {
		t.Errorf("Choose() = %d, want 2", result)
	}
}

func TestChoose_DefaultValue(t *testing.T) {
	restore := quiet(t)
	defer restore()

	scanner = bufio.NewScanner(strings.NewReader("\n"))
	defer func() { scanner = bufio.NewScanner(os.Stdin) }()

	options := []string{"Option A", "Option B", "Option C"}
	result := Choose("Pick one:", options, 1)
	if result != 1 {
		t.Errorf("Choose() with default = %d, want 1", result)
	}
}

func TestChoose_FirstOption(t *testing.T) {
	restore := quiet(t)
	defer restore()

	scanner = bufio.NewScanner(strings.NewReader("1\n"))
	defer func() { scanner = bufio.NewScanner(os.Stdin) }()

	options := []string{"First", "Second"}
	result := Choose("Pick:", options, 0)
	if result != 1 {
		t.Errorf("Choose() = %d, want 1", result)
	}
}

func TestMultiChoose_Single(t *testing.T) {
	restore := quiet(t)
	defer restore()

	scanner = bufio.NewScanner(strings.NewReader("2\n"))
	defer func() { scanner = bufio.NewScanner(os.Stdin) }()

	options := []string{"MySQL", "PostgreSQL", "Redis"}
	result := MultiChoose("DB:", options)
	if len(result) != 1 || result[0] != 2 {
		t.Errorf("MultiChoose() = %v, want [2]", result)
	}
}

func TestMultiChoose_Multiple(t *testing.T) {
	restore := quiet(t)
	defer restore()

	// First "0" triggers multi-select mode, then "1,3" selects options
	scanner = bufio.NewScanner(strings.NewReader("0\n1,3\n"))
	defer func() { scanner = bufio.NewScanner(os.Stdin) }()

	options := []string{"MySQL", "PostgreSQL", "Redis"}
	result := MultiChoose("DB:", options)
	if len(result) != 2 {
		t.Fatalf("MultiChoose() returned %d items, want 2", len(result))
	}
	if result[0] != 1 || result[1] != 3 {
		t.Errorf("MultiChoose() = %v, want [1,3]", result)
	}
}

func TestMultiChoose_Invalid(t *testing.T) {
	restore := quiet(t)
	defer restore()

	scanner = bufio.NewScanner(strings.NewReader("invalid\n"))
	defer func() { scanner = bufio.NewScanner(os.Stdin) }()

	options := []string{"MySQL", "PostgreSQL"}
	result := MultiChoose("DB:", options)
	if result != nil {
		t.Errorf("MultiChoose() with invalid input = %v, want nil", result)
	}
}

func TestMultiChoose_OutOfRange(t *testing.T) {
	restore := quiet(t)
	defer restore()

	scanner = bufio.NewScanner(strings.NewReader("99\n"))
	defer func() { scanner = bufio.NewScanner(os.Stdin) }()

	options := []string{"MySQL", "PostgreSQL"}
	result := MultiChoose("DB:", options)
	if result != nil {
		t.Errorf("MultiChoose() with out-of-range = %v, want nil", result)
	}
}

func TestAskMultiple(t *testing.T) {
	restore := quiet(t)
	defer restore()

	scanner = bufio.NewScanner(strings.NewReader("KEY1=VAL1\nKEY2=VAL2\n\n"))
	defer func() { scanner = bufio.NewScanner(os.Stdin) }()

	result := AskMultiple("Env vars")
	if len(result) != 2 {
		t.Fatalf("AskMultiple() returned %d items, want 2", len(result))
	}
	if result[0] != "KEY1=VAL1" {
		t.Errorf("result[0] = %q, want 'KEY1=VAL1'", result[0])
	}
	if result[1] != "KEY2=VAL2" {
		t.Errorf("result[1] = %q, want 'KEY2=VAL2'", result[1])
	}
}

func TestAskMultiple_Empty(t *testing.T) {
	restore := quiet(t)
	defer restore()

	scanner = bufio.NewScanner(strings.NewReader("\n"))
	defer func() { scanner = bufio.NewScanner(os.Stdin) }()

	result := AskMultiple("Env vars")
	if len(result) != 0 {
		t.Errorf("AskMultiple() with immediate empty = %v, want empty", result)
	}
}

func TestChoose_EmptyNoDefault(t *testing.T) {
	restore := quiet(t)
	defer restore()

	scanner = bufio.NewScanner(strings.NewReader("\n1\n"))
	defer func() { scanner = bufio.NewScanner(os.Stdin) }()

	options := []string{"A", "B"}
	result := Choose("Pick:", options, 0)
	if result != 1 {
		t.Errorf("Choose() = %d, want 1", result)
	}
}

func TestSetScannerForTesting(t *testing.T) {
	oldScanner := scanner
	defer func() { scanner = oldScanner }()

	newScanner := bufio.NewScanner(strings.NewReader("test\n"))
	SetScannerForTesting(newScanner)

	restore := quiet(t)
	defer restore()

	result := Ask("Name", "")
	if result != "test" {
		t.Errorf("SetScannerForTesting() did not replace scanner correctly, got %q", result)
	}
}

func TestAsk_ScannerEOF(t *testing.T) {
	restore := quiet(t)
	defer restore()

	scanner = bufio.NewScanner(strings.NewReader(""))
	defer func() { scanner = bufio.NewScanner(os.Stdin) }()

	result := Ask("Name", "fallback")
	if result != "fallback" {
		t.Errorf("Ask() with EOF = %q, want 'fallback'", result)
	}
}

func TestConfirm_ScannerEOF(t *testing.T) {
	restore := quiet(t)
	defer restore()

	scanner = bufio.NewScanner(strings.NewReader(""))
	defer func() { scanner = bufio.NewScanner(os.Stdin) }()

	result := Confirm("Continue?", false)
	if result != false {
		t.Errorf("Confirm() with EOF = %v, want false", result)
	}
}

func TestChoose_InvalidThenValid(t *testing.T) {
	restore := quiet(t)
	defer restore()

	scanner = bufio.NewScanner(strings.NewReader("99\n0\n2\n"))
	defer func() { scanner = bufio.NewScanner(os.Stdin) }()

	options := []string{"A", "B"}
	result := Choose("Pick:", options, 0)
	if result != 2 {
		t.Errorf("Choose() = %d, want 2", result)
	}
}

func TestAskMultiple_ScannerEOF(t *testing.T) {
	restore := quiet(t)
	defer restore()

	scanner = bufio.NewScanner(strings.NewReader("item1"))
	defer func() { scanner = bufio.NewScanner(os.Stdin) }()

	result := AskMultiple("Items")
	if len(result) != 1 || result[0] != "item1" {
		t.Errorf("AskMultiple() with EOF = %v, want ['item1']", result)
	}
}
