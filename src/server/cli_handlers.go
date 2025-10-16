package server

import (
	"fmt"
	"net/http"
	"strings"
)

// handleCLIScriptSh generates POSIX shell script
func (s *Server) handleCLIScriptSh(w http.ResponseWriter, r *http.Request) {
	// Get server URL (auto-detected from reverse proxy headers, public IP, or hostname)
	serverURL := s.detectServerURL(r)

	// Get default templates from query param
	defaults := r.URL.Query().Get("defaults")
	if defaults == "" {
		defaults = "linux,macos,windows"
	}

	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Content-Disposition", "attachment; filename=gitignore")

	// Generate POSIX shell script
	script := fmt.Sprintf(`#!/bin/sh
# gitignore CLI script
# Server: %s
# Generated: $(date)

SERVER_URL="%s"

# DEFAULT_TEMPLATES_START
# Edit these lines to customize your defaults
# Used when running 'gitignore' with no arguments
%s
# DEFAULT_TEMPLATES_END

# Show usage
show_usage() {
	echo "Usage: gitignore [options] [template...]"
	echo ""
	echo "Commands:"
	echo "  list              List all available templates"
	echo "  search <query>    Search for templates"
	echo "  update            Update this script"
	echo "  version           Show version"
	echo "  help              Show this help"
	echo ""
	echo "Options:"
	echo "  --force, -f       Overwrite .gitignore completely"
	echo "  --stdout, -o      Print to stdout"
	echo "  --dry-run, -d     Show what would be done"
	echo ""
	echo "Examples:"
	echo "  gitignore go linux vscode"
	echo "  gitignore go,python,macos"
	echo "  gitignore                    # Uses defaults"
}

# Handle commands
case "$1" in
	list)
		curl -LSs "$SERVER_URL/api/v1/list"
		;;
	search)
		shift
		curl -LSs "$SERVER_URL/api/v1/search?q=$*"
		;;
	update)
		echo "Updating gitignore script..."
		curl -LSs "$SERVER_URL/api/v1/cli/sh" > "$0.new" && \
			mv "$0.new" "$0" && \
			chmod +x "$0" && \
			echo "✅ Updated successfully!"
		;;
	version)
		echo "gitignore CLI"
		echo "Server: $SERVER_URL"
		;;
	help|--help|-h)
		show_usage
		;;
	*)
		# Generate .gitignore
		templates="$*"
		if [ -z "$templates" ]; then
			# Read defaults from this script
			templates=$(sed -n '/# DEFAULT_TEMPLATES_START/,/# DEFAULT_TEMPLATES_END/p' "$0" | \
				grep -v '^#' | tr '\n' ',' | sed 's/,$//')
		fi

		if [ -z "$templates" ]; then
			echo "❌ No templates specified"
			show_usage
			exit 1
		fi

		# Replace spaces with commas
		templates=$(echo "$templates" | tr ' ' ',')

		echo "🎯 Fetching templates: $templates"
		curl -LSs "$SERVER_URL/api/v1/combine?templates=$templates" > .gitignore
		echo "✅ Created .gitignore"
		;;
esac
`, serverURL, serverURL, defaults)

	w.Write([]byte(script))
}

// handleCLIScriptPs generates PowerShell script
func (s *Server) handleCLIScriptPs(w http.ResponseWriter, r *http.Request) {
	// Get server URL (auto-detected from reverse proxy headers, public IP, or hostname)
	serverURL := s.detectServerURL(r)

	// Get default templates
	defaults := r.URL.Query().Get("defaults")
	if defaults == "" {
		defaults = "windows,visualstudio,vscode"
	}

	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Content-Disposition", "attachment; filename=gitignore.ps1")

	// Generate PowerShell script
	script := fmt.Sprintf(`# gitignore CLI script (PowerShell)
# Server: %s
# Generated: $(Get-Date)

$SERVER_URL = "%s"

# DEFAULT_TEMPLATES_START
%s
# DEFAULT_TEMPLATES_END

function Show-Usage {
	Write-Host "Usage: gitignore [options] [template...]"
	Write-Host ""
	Write-Host "Commands:"
	Write-Host "  list              List all available templates"
	Write-Host "  search <query>    Search for templates"
	Write-Host "  update            Update this script"
	Write-Host "  version           Show version"
	Write-Host "  help              Show this help"
}

# Handle commands
switch ($args[0]) {
	"list" {
		Invoke-RestMethod -Uri "$SERVER_URL/api/v1/list"
	}
	"search" {
		$query = $args[1..($args.Length-1)] -join " "
		Invoke-RestMethod -Uri "$SERVER_URL/api/v1/search?q=$query"
	}
	"update" {
		Write-Host "Updating gitignore script..."
		Invoke-WebRequest -Uri "$SERVER_URL/api/v1/cli/ps" -OutFile "$PSCommandPath.new"
		Move-Item -Path "$PSCommandPath.new" -Destination $PSCommandPath -Force
		Write-Host "✅ Updated successfully!"
	}
	"version" {
		Write-Host "gitignore CLI (PowerShell)"
		Write-Host "Server: $SERVER_URL"
	}
	"help" {
		Show-Usage
	}
	default {
		$templates = $args -join ","
		if ([string]::IsNullOrEmpty($templates)) {
			# Read defaults from this script
			$templates = "%s"
		}

		Write-Host "🎯 Fetching templates: $templates"
		Invoke-RestMethod -Uri "$SERVER_URL/api/v1/combine?templates=$templates" | Out-File -FilePath .gitignore -Encoding UTF8
		Write-Host "✅ Created .gitignore"
	}
}
`, serverURL, serverURL, defaults, defaults)

	w.Write([]byte(script))
}

// handleCLICompletionBash generates Bash completion script
func (s *Server) handleCLICompletionBash(w http.ResponseWriter, r *http.Request) {
	templates := strings.Join(s.config.Templates.List(), " ")

	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Content-Disposition", "attachment; filename=gitignore-completion.bash")

	completion := fmt.Sprintf(`# Bash completion for gitignore
_gitignore_completions() {
	local cur prev
	cur="${COMP_WORDS[COMP_CWORD]}"
	prev="${COMP_WORDS[COMP_CWORD-1]}"

	# Commands
	if [ $COMP_CWORD -eq 1 ]; then
		COMPREPLY=( $(compgen -W "list search update version help" -- "$cur") )
		return 0
	fi

	# Template names
	COMPREPLY=( $(compgen -W "%s" -- "$cur") )
}

complete -F _gitignore_completions gitignore
`, templates)

	w.Write([]byte(completion))
}

// handleCLICompletionZsh generates Zsh completion script
func (s *Server) handleCLICompletionZsh(w http.ResponseWriter, r *http.Request) {
	templates := s.config.Templates.List()

	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Content-Disposition", "attachment; filename=_gitignore")

	templateList := ""
	for _, tmpl := range templates {
		templateList += fmt.Sprintf("'%s' ", tmpl)
	}

	completion := fmt.Sprintf(`#compdef gitignore

_gitignore() {
	local -a commands templates
	commands=(
		'list:List all available templates'
		'search:Search templates'
		'update:Update the gitignore script'
		'version:Show version'
		'help:Show help'
	)
	templates=(%s)

	_arguments \
		'1: :->command' \
		'*: :->templates'

	case $state in
		command)
			_describe 'command' commands
			;;
		templates)
			_describe 'template' templates
			;;
	esac
}
`, templateList)

	w.Write([]byte(completion))
}

// handleCLICompletionFish generates Fish completion script
func (s *Server) handleCLICompletionFish(w http.ResponseWriter, r *http.Request) {
	templates := s.config.Templates.List()

	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Content-Disposition", "attachment; filename=gitignore.fish")

	completion := `# Fish completion for gitignore

# Commands
complete -c gitignore -n "__fish_use_subcommand" -a "list" -d "List all templates"
complete -c gitignore -n "__fish_use_subcommand" -a "search" -d "Search templates"
complete -c gitignore -n "__fish_use_subcommand" -a "update" -d "Update script"
complete -c gitignore -n "__fish_use_subcommand" -a "version" -d "Show version"
complete -c gitignore -n "__fish_use_subcommand" -a "help" -d "Show help"

# Template names
`
	for _, tmpl := range templates {
		completion += fmt.Sprintf("complete -c gitignore -a \"%s\" -d \"%s template\"\n", tmpl, tmpl)
	}

	w.Write([]byte(completion))
}
