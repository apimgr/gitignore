package cmd

import (
	"bytes"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/apimgr/gitignore/src/client/api"
	"github.com/apimgr/gitignore/src/client/output"
)

func captureOutput(t *testing.T, run func()) string {
	t.Helper()
	old := os.Stdout
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = writer
	run()
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	os.Stdout = old
	var buffer bytes.Buffer
	if _, err := io.Copy(&buffer, reader); err != nil {
		t.Fatal(err)
	}
	_ = reader.Close()
	return buffer.String()
}

func TestDispatchHelpAndUsageBranches(t *testing.T) {
	printer := output.New(false)
	if got := Dispatch(api.New("http://127.0.0.1:1"), printer, "text", []string{"category"}); got != output.ExitUsage {
		t.Fatalf("category without name returned %d", got)
	}
	if got := Dispatch(api.New("http://127.0.0.1:1"), printer, "text", []string{"get"}); got != output.ExitUsage {
		t.Fatalf("get without name returned %d", got)
	}
	help := captureOutput(t, func() {
		if got := Dispatch(nil, printer, "text", []string{"help"}); got != output.ExitSuccess {
			t.Errorf("help returned %d", got)
		}
	})
	if !strings.Contains(help, "Usage:") {
		t.Fatalf("help output missing usage: %s", help)
	}
}

func TestInteractiveInputAndHelp(t *testing.T) {
	oldInput, oldBinaryName := os.Stdin, BinaryName
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := io.WriteString(writer, "help\nquit\n"); err != nil {
		t.Fatal(err)
	}
	_ = writer.Close()
	os.Stdin = reader
	BinaryName = "gitignore-test"
	t.Cleanup(func() {
		os.Stdin = oldInput
		BinaryName = oldBinaryName
		_ = reader.Close()
	})
	outputText := captureOutput(t, func() {
		if got := Interactive(nil, output.New(false), "text"); got != output.ExitSuccess {
			t.Errorf("Interactive returned %d", got)
		}
	})
	if !strings.Contains(strings.ToLower(outputText), "interactive mode") || !strings.Contains(outputText, "Commands:") {
		t.Fatalf("interactive help output incomplete: %s", outputText)
	}
}

func TestInteractiveEOFAndPrintVersionWithoutClient(t *testing.T) {
	oldInput := os.Stdin
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	_ = writer.Close()
	os.Stdin = reader
	t.Cleanup(func() { os.Stdin = oldInput; _ = reader.Close() })
	if got := Interactive(nil, output.New(false), "text"); got != output.ExitSuccess {
		t.Fatalf("Interactive at EOF returned %d", got)
	}
	version := captureOutput(t, func() { PrintVersion("1.2.3", "abc1234", "now", nil) })
	if !strings.Contains(version, "1.2.3") || !strings.Contains(version, "abc1234") {
		t.Fatalf("version output incomplete: %s", version)
	}
}
