package cmd

import (
	"fmt"
	"os"
	"path/filepath"
)

// commandWords lists the CLI's subcommands for shell completion generation.
var commandWords = []string{"list", "search", "categories", "category", "stats", "get", "template", "combine", "help"}

// DetectShell extracts a shell name from $SHELL (e.g. "/bin/zsh" -> "zsh"),
// defaulting to "bash" when unset.
func DetectShell() string {
	shellPath := os.Getenv("SHELL")
	if shellPath == "" {
		return "bash"
	}
	return filepath.Base(shellPath)
}

// HandleShell implements --shell completions|init|help [SHELL].
func HandleShell(args []string) int {
	if len(args) < 1 {
		fmt.Fprintln(os.Stderr, "Usage: --shell [completions|init|help] [SHELL]")
		return 64
	}

	sub := args[0]
	shell := ""
	if len(args) > 1 {
		shell = args[1]
	} else {
		shell = DetectShell()
	}

	binaryName := filepath.Base(os.Args[0])

	switch sub {
	case "completions":
		return printCompletions(shell, binaryName)
	case "init":
		return printInit(shell, binaryName)
	case "help":
		fmt.Printf("Shell integration for %s:\n", binaryName)
		fmt.Println("  --shell completions [SHELL]  Print shell completions")
		fmt.Println("  --shell init [SHELL]         Print shell init command")
		fmt.Println("  SHELL: bash, zsh, fish, sh, dash, ksh, powershell, pwsh (auto-detect if omitted)")
		return 0
	default:
		fmt.Fprintln(os.Stderr, "Usage: --shell [completions|init|help] [SHELL]")
		return 64
	}
}

func printCompletions(shell, binaryName string) int {
	switch shell {
	case "bash":
		fmt.Print(generateBashCompletions(binaryName))
	case "zsh":
		fmt.Print(generateZshCompletions(binaryName))
	case "fish":
		fmt.Print(generateFishCompletions(binaryName))
	case "sh", "dash", "ksh":
		fmt.Print(generatePosixCompletions(binaryName))
	case "powershell", "pwsh":
		fmt.Print(generatePowershellCompletions(binaryName))
	default:
		fmt.Fprintf(os.Stderr, "Error: unsupported shell: %s\n", shell)
		return 64
	}
	return 0
}

func printInit(shell, binaryName string) int {
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
		fmt.Fprintf(os.Stderr, "Error: unsupported shell: %s\n", shell)
		return 64
	}
	return 0
}

func generateBashCompletions(bin string) string {
	words := joinWords(commandWords)
	return fmt.Sprintf(`# Bash completion for %[1]s
_%[1]s_completions() {
	local cur prev
	cur="${COMP_WORDS[COMP_CWORD]}"
	prev="${COMP_WORDS[COMP_CWORD-1]}"
	if [ "$COMP_CWORD" -eq 1 ]; then
		COMPREPLY=( $(compgen -W "%[2]s" -- "$cur") )
		return 0
	fi
	COMPREPLY=( $(compgen -W "--help --version --server --config --output --color --debug --lang" -- "$cur") )
}
complete -F _%[1]s_completions %[1]s
`, bin, words)
}

func generateZshCompletions(bin string) string {
	words := joinWords(commandWords)
	return fmt.Sprintf(`#compdef %[1]s
_%[1]s() {
	local -a commands
	commands=(%[2]s)
	_describe 'command' commands
}
_%[1]s
`, bin, words)
}

func generateFishCompletions(bin string) string {
	var b []byte
	for _, w := range commandWords {
		b = append(b, []byte(fmt.Sprintf("complete -c %s -n \"__fish_use_subcommand\" -a \"%s\"\n", bin, w))...)
	}
	b = append(b, []byte(fmt.Sprintf("complete -c %s -l help -d 'Show help'\n", bin))...)
	b = append(b, []byte(fmt.Sprintf("complete -c %s -l version -d 'Show version'\n", bin))...)
	return string(b)
}

func generatePosixCompletions(bin string) string {
	return fmt.Sprintf("# POSIX completion for %s is not standardized; supported commands: %s\n", bin, joinWords(commandWords))
}

func generatePowershellCompletions(bin string) string {
	words := joinWords(commandWords)
	return fmt.Sprintf(`Register-ArgumentCompleter -Native -CommandName %s -ScriptBlock {
	param($wordToComplete, $commandAst, $cursorPosition)
	@(%s) -split ' ' | Where-Object { $_ -like "$wordToComplete*" } | ForEach-Object { [System.Management.Automation.CompletionResult]::new($_, $_, 'ParameterValue', $_) }
}
`, bin, words)
}

func joinWords(words []string) string {
	out := ""
	for i, w := range words {
		if i > 0 {
			out += " "
		}
		out += w
	}
	return out
}
