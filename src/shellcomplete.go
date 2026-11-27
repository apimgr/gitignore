package main

import (
	"fmt"
	"os"
	"path/filepath"
)

// serverFlagWords lists the server's CLI flags for shell completion generation
// (AI.md PART 8 "--shell").
var serverFlagWords = []string{
	"--port", "--address", "--config", "--data", "--cache", "--log", "--backup",
	"--pid", "--baseurl", "--i2p", "--daemon", "--mode", "--service", "--maintenance",
	"--status", "--update", "--debug", "--color", "--force", "--version", "--help",
	"--shell",
}

// detectShell extracts a shell name from $SHELL (e.g. "/bin/zsh" -> "zsh"),
// defaulting to "bash" when unset.
func detectShell() string {
	shellPath := os.Getenv("SHELL")
	if shellPath == "" {
		return "bash"
	}
	return filepath.Base(shellPath)
}

// handleShell implements --shell completions|init|help [SHELL] for the server
// binary and returns the process exit code.
func handleShell(args []string) int {
	if len(args) < 1 {
		fmt.Fprintln(os.Stderr, cliText("cli.shell.usage"))
		return 2
	}

	sub := args[0]
	shell := ""
	if len(args) > 1 {
		shell = args[1]
	} else {
		shell = detectShell()
	}

	binaryName := filepath.Base(os.Args[0])

	switch sub {
	case "completions":
		return printShellCompletions(shell, binaryName)
	case "init":
		return printShellInit(shell, binaryName)
	case "help", "--help":
		fmt.Printf("%s\n", cliText("cli.shell.help_header", "binary", binaryName))
		fmt.Printf("  --shell completions [SHELL]  %s\n", cliText("cli.shell.completions"))
		fmt.Printf("  --shell init [SHELL]         %s\n", cliText("cli.shell.init"))
		fmt.Printf("  %s\n", cliText("cli.shell.supported_shells", "shells", "bash, zsh, fish, sh, dash, ksh, powershell, pwsh"))
		return 0
	default:
		fmt.Fprintln(os.Stderr, cliText("cli.shell.usage"))
		return 2
	}
}

func printShellCompletions(shell, binaryName string) int {
	switch shell {
	case "bash":
		fmt.Print(generateServerBashCompletions(binaryName))
	case "zsh":
		fmt.Print(generateServerZshCompletions(binaryName))
	case "fish":
		fmt.Print(generateServerFishCompletions(binaryName))
	case "sh", "dash", "ksh":
		fmt.Print(generateServerPosixCompletions(binaryName))
	case "powershell", "pwsh":
		fmt.Print(generateServerPowershellCompletions(binaryName))
	default:
		fmt.Fprintln(os.Stderr, cliText("cli.error_unsupported_shell", "shell", shell))
		return 2
	}
	return 0
}

func printShellInit(shell, binaryName string) int {
	switch shell {
	case "bash":
		fmt.Printf("source <(%s --shell completions bash)\n", binaryName)
	case "zsh":
		fmt.Printf("source <(%s --shell completions zsh)\n", binaryName)
	case "fish":
		fmt.Printf("%s --shell completions fish | source\n", binaryName)
	case "sh", "dash", "ksh":
		fmt.Printf("eval \"$(%s --shell completions %s)\"\n", binaryName, shell)
	case "powershell", "pwsh":
		fmt.Printf("Invoke-Expression (& %s --shell completions powershell)\n", binaryName)
	default:
		fmt.Fprintln(os.Stderr, cliText("cli.error_unsupported_shell", "shell", shell))
		return 2
	}
	return 0
}

func generateServerBashCompletions(bin string) string {
	return fmt.Sprintf(`# %[3]s
_%[1]s_completions() {
	local cur
	cur="${COMP_WORDS[COMP_CWORD]}"
	COMPREPLY=( $(compgen -W "%[2]s" -- "$cur") )
}
complete -F _%[1]s_completions %[1]s
`, bin, joinServerWords(serverFlagWords), cliText("cli.shell.bash_comment", "binary", bin))
}

func generateServerZshCompletions(bin string) string {
	return fmt.Sprintf(`#compdef %[1]s
_%[1]s() {
	local -a flags
	flags=(%[2]s)
	_describe 'flag' flags
}
_%[1]s
`, bin, joinServerWords(serverFlagWords))
}

func generateServerFishCompletions(bin string) string {
	var b []byte
	for _, w := range serverFlagWords {
		name := w
		for len(name) > 0 && name[0] == '-' {
			name = name[1:]
		}
		b = append(b, []byte(fmt.Sprintf("complete -c %s -l %s\n", bin, name))...)
	}
	return string(b)
}

func generateServerPosixCompletions(bin string) string {
	return fmt.Sprintf("# %s\n", cliText("cli.shell.posix_comment", "binary", bin, "flags", joinServerWords(serverFlagWords)))
}

func generateServerPowershellCompletions(bin string) string {
	return fmt.Sprintf(`Register-ArgumentCompleter -Native -CommandName %s -ScriptBlock {
	param($wordToComplete, $commandAst, $cursorPosition)
	@(%s) -split ' ' | Where-Object { $_ -like "$wordToComplete*" } | ForEach-Object { [System.Management.Automation.CompletionResult]::new($_, $_, 'ParameterValue', $_) }
}
`, bin, joinServerWords(serverFlagWords))
}

func joinServerWords(words []string) string {
	out := ""
	for i, w := range words {
		if i > 0 {
			out += " "
		}
		out += w
	}
	return out
}
