# 🚫 GitIgnore API Server - Project Specification

**Project**: gitignore
**Module**: github.com/apimgr/gitignore
**Language**: Go 1.21+
**Purpose**: Comprehensive .gitignore template API with admin-protected server configuration
**Data**: Extensive collection of .gitignore templates for various languages, frameworks, and tools

---

## 📖 Table of Contents

1. [Project Overview](#project-overview)
2. [Architecture](#architecture)
3. [Directory Layout](#directory-layout)
4. [Data Sources](#data-sources)
5. [CLI Scripts](#cli-scripts)
6. [Authentication](#authentication)
7. [Routes & Endpoints](#routes--endpoints)
8. [Configuration](#configuration)
9. [Frontend & UI](#frontend--ui)
10. [URL Detection & Server Address](#url-detection--server-address)
11. [Reverse Proxy Support](#reverse-proxy-support)
12. [Build & Deployment](#build--deployment)
13. [Development](#development)
14. [Testing](#testing)

---

## 🎯 Project Overview

### What This Is

A **public .gitignore template API** with a web frontend, built as a single self-contained Go binary.

- **Public API**: All .gitignore templates are freely accessible (no authentication)
- **Admin Interface**: Server configuration protected by token/password authentication
- **Embedded Data**: .gitignore templates built into binary
- **Fast Search**: In-memory indexes for instant lookups
- **Template Combinations**: Merge multiple templates into a single .gitignore file
- **Web Frontend**: Go html/template based UI with Dracula theme
- **Modern UI**: Modals, toggles, buttons, responsive design
- **Export Formats**: Plain text, JSON

### Key Features

- Search templates by language, framework, IDE, OS
- Combine multiple templates into one .gitignore file
- Browse templates by category
- Preview and download templates
- RESTful API with matching web/API routes
- Admin dashboard for server configuration
- Single binary deployment

---

## 🏗️ Architecture

### System Design

```
┌─────────────────────────────────────────┐
│         Single Go Binary                │
│  ┌─────────────────────────────────┐   │
│  │  Embedded Assets (go:embed)     │   │
│  │  • .gitignore templates         │   │
│  │  • HTML templates               │   │
│  │  • CSS/JS/Images                │   │
│  └─────────────────────────────────┘   │
│  ┌─────────────────────────────────┐   │
│  │  In-Memory Data Structures      │   │
│  │  • Template maps/indexes        │   │
│  │  • Category indexes             │   │
│  └─────────────────────────────────┘   │
│  ┌─────────────────────────────────┐   │
│  │  HTTP Server (Chi Router)       │   │
│  │  • Public routes (no auth)      │   │
│  │  • Admin routes (auth required) │   │
│  │  • API v1 endpoints             │   │
│  └─────────────────────────────────┘   │
│  ┌─────────────────────────────────┐   │
│  │  SQLite Database                │   │
│  │  • Admin credentials (hashed)   │   │
│  │  • Server settings              │   │
│  └─────────────────────────────────┘   │
└─────────────────────────────────────────┘
```

### Technology Stack

- **Language**: Go 1.21+
- **HTTP Router**: Chi v5
- **Database**: SQLite (modernc.org/sqlite - pure Go, no CGO)
- **Templates**: Go html/template
- **Embedding**: Go embed.FS
- **Authentication**: SHA-256 hashing, Bearer tokens, Basic Auth
- **API Documentation**: Swagger/OpenAPI v3 + GraphQL
- **Content Negotiation**: Plain text by default, JSON when requested or browser detected

---

## 📁 Directory Layout

### OS-Specific Paths

```yaml
Linux/BSD (with root privileges):
  Config:  /etc/gitignore/
  Data:    /var/lib/gitignore/
  Logs:    /var/log/gitignore/
  Runtime: /run/gitignore/

Linux/BSD (without root):
  Config:  ~/.config/gitignore/
  Data:    ~/.local/share/gitignore/
  Logs:    ~/.local/state/gitignore/
  Runtime: ~/.local/run/gitignore/

macOS (with privileges):
  Config:  /Library/Application Support/GitIgnore/
  Data:    /Library/Application Support/GitIgnore/data/
  Logs:    /Library/Logs/GitIgnore/
  Runtime: /var/run/gitignore/

macOS (without privileges):
  Config:  ~/Library/Application Support/GitIgnore/
  Data:    ~/Library/Application Support/GitIgnore/data/
  Logs:    ~/Library/Logs/GitIgnore/
  Runtime: ~/Library/Application Support/GitIgnore/run/

Windows:
  Config:  C:\ProgramData\GitIgnore\config\
  Data:    C:\ProgramData\GitIgnore\data\
  Logs:    C:\ProgramData\GitIgnore\logs\
  Runtime: C:\ProgramData\GitIgnore\run\

Windows (user):
  Config:  %APPDATA%\GitIgnore\config\
  Data:    %APPDATA%\GitIgnore\data\
  Logs:    %APPDATA%\GitIgnore\logs\
  Runtime: %APPDATA%\GitIgnore\run\

Docker:
  Config:  /config
  Data:    /data
  Logs:    /data/logs
  Runtime: /tmp
```

### Directory Contents

```yaml
Config Directory:
  - admin_credentials     # Generated on first run (0600 permissions)
  - settings.db          # SQLite database (if separate from data)

Data Directory:
  - gitignore.db         # SQLite database (default location)
  - backups/             # Automatic backups
  - cache/               # Optional cache files

Logs Directory:
  - access.log           # HTTP access logs
  - error.log            # Application errors
  - audit.log            # Admin actions

Runtime Directory:
  - gitignore.pid        # Process ID file
  - gitignore.sock       # Unix socket (optional)
```

### Environment Variables & Flags

```yaml
Directory Overrides (in priority order):
  1. Command-line flags
  2. Environment variables
  3. OS defaults

Flags:
  --config DIR        # Configuration directory
  --data DIR          # Data directory
  --logs DIR          # Logs directory

  --port PORT         # HTTP port (default: random 64000-64999)
  --address ADDR      # Listen address (default: 0.0.0.0)

  --db-type TYPE      # Database type: sqlite, mysql, postgres
  --db-path PATH      # SQLite database path
  --db-url URL        # Database connection string

Environment Variables:
  CONFIG_DIR          # Configuration directory
  DATA_DIR            # Data directory
  LOGS_DIR            # Logs directory

  PORT                # Server port
  ADDRESS             # Listen address

  DATABASE_URL        # Full connection string
  DB_TYPE             # Database type
  DB_PATH             # SQLite path

  ADMIN_USER          # Admin username (first run only)
  ADMIN_PASSWORD      # Admin password (first run only)
  ADMIN_TOKEN         # Admin API token (first run only)

Docker Environment:
  Mounted volumes:
    -v ./config:/config
    -v ./data:/data

  Environment:
    -e CONFIG_DIR=/config
    -e DATA_DIR=/data
    -e PORT=8080
    -e ADMIN_PASSWORD=changeme
```

### Project Source Layout

```
./
├── src/                   # All Go source files
│   ├── templates/         # GitIgnore template service package
│   │   ├── data/
│   │   │   └── gitignore/ # Embedded .gitignore templates
│   │   ├── data.go        # Template loading & indexing
│   │   └── handlers.go    # HTTP handlers
│   ├── database/          # Database package
│   │   ├── database.go    # Connection & schema
│   │   ├── settings.go    # Settings CRUD
│   │   └── auth.go        # Admin auth
│   ├── paths/             # OS path detection
│   │   └── paths.go       # OS-specific directory resolution
│   └── server/            # HTTP server package
│       ├── server.go          # Server setup & routing
│       ├── auth_middleware.go # Auth middleware
│       ├── admin_handlers.go  # Admin route handlers
│       ├── handlers.go        # Public handlers
│       ├── web_handlers.go    # Web page handlers (html/template)
│       ├── cli_handlers.go    # CLI script generation handlers
│       ├── static/            # Embedded static files
│       │   ├── css/
│       │   ├── js/
│       │   └── images/
│       └── templates/         # Embedded HTML templates (html/template)
│           ├── base.html
│           ├── home.html
│           ├── search.html
│           ├── template.html
│           ├── cli.html       # CLI customization page (interactive form)
│           ├── cli/           # CLI script templates
│           │   ├── gitignore.sh.tmpl        # POSIX shell script template
│           │   ├── gitignore.ps1.tmpl       # PowerShell script template
│           │   ├── completion.bash.tmpl     # Bash completion script
│           │   ├── completion.zsh.tmpl      # Zsh completion script
│           │   └── completion.fish.tmpl     # Fish completion script
│           └── admin/
│               ├── dashboard.html
│               └── settings.html
├── scripts/               # Production scripts (self-contained)
│   ├── install.sh         # Installation script
│   └── backup.sh          # Backup script
├── tests/                 # Development & test scripts
│   ├── test-docker.sh     # Docker testing script
│   ├── unit/              # Unit tests
│   ├── integration/       # Integration tests
│   └── e2e/               # End-to-end tests
├── binaries/              # Built binaries (gitignored)
│   ├── gitignore-linux-amd64
│   ├── gitignore-linux-arm64
│   ├── gitignore-windows-amd64.exe
│   ├── gitignore-windows-arm64.exe
│   ├── gitignore-macos-amd64
│   ├── gitignore-macos-arm64
│   ├── gitignore-bsd-amd64
│   └── gitignore          # Current platform
├── release/               # Release artifacts (gitignored)
│   ├── gitignore-v1.0.0-linux-amd64.tar.gz
│   ├── gitignore-v1.0.0-windows-amd64.zip
│   ├── checksums.txt
│   └── release-notes.md
├── rootfs/                # Docker persistent volumes (gitignored)
│   ├── config/            # → /config (in container)
│   ├── data/              # → /data (in container)
│   ├── logs/              # → /logs (in container)
│   └── db/                # External databases
│       ├── postgres/
│       └── mysql/
├── main.go                # Entry point
├── go.mod
├── go.sum
├── Makefile
├── Dockerfile
├── docker-compose.yml      # Production compose
├── docker-compose.test.yml # Testing compose (/tmp volumes)
├── CLAUDE.md              # This file (specification)
├── docs/                  # Documentation
│   ├── README.md          # Documentation index
│   ├── SERVER.md          # Server administration guide
│   └── API.md             # Complete API documentation
└── README.md              # User documentation
```

---

## 💾 Data Sources

### GitIgnore Templates

```yaml
Source: GitHub's official gitignore collection (github.com/github/gitignore)
Location: src/templates/data/gitignore/
Format: Individual .gitignore files organized by category
Embedded: Yes (go:embed)
Total Templates: 500+ templates covering all major technologies

Categories (following GitHub's structure):
  - Root: Popular programming languages and technologies
  - Global: Editors, tools, and operating systems
  - Community: Specialized templates for frameworks/tools/projects

Examples Include:
  - Languages: Go, Python, Java, JavaScript, C++, Rust, Ruby, PHP, etc.
  - Frameworks: Node, Django, Rails, Laravel, Angular, React, Vue, etc.
  - IDEs: VSCode, IntelliJ, Vim, Emacs, Eclipse, Xcode, etc.
  - Operating Systems: macOS, Windows, Linux
  - Tools: Docker, Terraform, Ansible, CMake, Maven, etc.

Structure:
  src/templates/data/gitignore/
    ├── Global/              # OS, Editors, Tools (from GitHub Global/)
    │   ├── macOS.gitignore
    │   ├── Windows.gitignore
    │   ├── Linux.gitignore
    │   ├── VisualStudioCode.gitignore
    │   ├── Vim.gitignore
    │   └── ...
    ├── Go.gitignore         # Root templates (popular languages)
    ├── Python.gitignore
    ├── Java.gitignore
    ├── Node.gitignore
    └── Community/           # Specialized templates (from GitHub Community/)
        ├── Rails.gitignore
        ├── Laravel.gitignore
        └── ...

Template Naming:
  - Case-sensitive filenames matching GitHub convention
  - Remove .gitignore extension for template names in API
  - Example: "Go.gitignore" → template name "go" (case-insensitive lookup)

Indexes Built on Startup:
  - By template name (case-insensitive)
  - By category (Global, Root, Community)
  - By tags/keywords extracted from comments
  - Full-text search index for quick lookups

Content Negotiation:
  - Default: text/plain (.gitignore content)
  - With Accept: application/json → JSON metadata
  - Browser User-Agent → HTML page
  - Multiple templates → combined text/plain by default
```

---

## 🖥️ CLI Scripts

### Overview

The server dynamically generates shell scripts that users can install locally to interact with the API from the command line. Scripts are customized with the server's URL and provide a seamless CLI experience.

### Script Generation

```yaml
POSIX Shell Script (/api/v1/cli/sh):
  - POSIX-compliant (bash, zsh, sh, dash)
  - Works on Linux, macOS, BSD, WSL
  - Embedded server URL (dynamic)
  - Single-file, no dependencies
  - Script name: gitignore
  - Template: src/server/templates/cli/gitignore.sh.tmpl
  - Query param: ?defaults=template1,template2 (optional)

PowerShell Script (/api/v1/cli/ps):
  - Windows PowerShell 5.1+ compatible
  - Also works on PowerShell Core (cross-platform)
  - Embedded server URL (dynamic)
  - Single-file, no dependencies
  - Script name: gitignore
  - Template: src/server/templates/cli/gitignore.ps1.tmpl
  - Query param: ?defaults=template1,template2 (optional)

CLI Customization Page (/cli):
  - Interactive web form for script customization
  - Template selector with search/filter
  - Live preview of embedded defaults
  - Platform-specific install commands
  - One-click download with custom defaults
  - Template: src/server/templates/cli.html
```

### Script Features

```yaml
Commands (both POSIX and PowerShell):

  gitignore list
    - 📋 List all available templates
    - Calls: GET /api/v1/list
    - Pretty output with emojis and categories

  gitignore search <query>
    - 🔍 Search templates by name/keyword
    - Calls: GET /api/v1/search?q=<query>
    - Shows matching templates with descriptions

  gitignore [template1[,template2,...]] [template3] [...] [flags]
    - ✨ Generate/update .gitignore file (DEFAULT ACTION)
    - Auto-detects git repository root or uses current directory
    - Creates or updates .gitignore file intelligently with deduplication
    - Calls: GET /api/v1/combine?templates=<list>
    - Supports comma-separated and space-separated templates
    - Reads defaults from script itself (embedded section)

    Behavior:
      • Detects if current directory is in a git repo
      • If git repo: operates on repo root .gitignore
      • If not git repo: operates on current directory .gitignore
      • Default: Smart merge with deduplication (safe)
      • With --force/-f: Overwrites entire .gitignore file (destructive)
      • Creates file if it doesn't exist
      • Uses CLI markers to track managed sections
      • Preserves user-added custom rules outside markers

    Smart Merge Features:
      • Finds CLI-managed section via markers
      • Deduplicates patterns (no repeated entries)
      • Tracks which templates are already applied
      • Merges new templates with existing ones
      • Updates timestamp and template list in marker
      • Preserves user's custom rules before/after CLI section
      • Only writes new patterns that don't already exist

    Default Templates (Embedded in Script):
      • Script reads itself to find DEFAULT_TEMPLATES section
      • Used when running 'gitignore' with no arguments
      • Embedded between special markers in the script file
      • Users can edit /usr/local/bin/gitignore to customize defaults
      • No separate config file needed - everything in one script
      • Can be overridden by explicitly specifying templates

    Flags:
      --force, -f     - Overwrite .gitignore completely (destructive, warns first)
      --stdout, -o    - Print to stdout instead of file (no modification)
      --dry-run, -d   - Show what would be done without modifying
      --append, -a    - Explicitly append (default behavior with smart merge)

    Examples:
      • gitignore go                    → 🎯 Updates .gitignore with go template
      • gitignore go,python,vscode      → 🎯 Comma-separated templates
      • gitignore go python vscode      → 🎯 Space-separated templates
      • gitignore go,node linux,macos   → 🎯 Mixed comma and space separation
      • gitignore                       → 🎯 Uses defaults from /usr/local/bin/.gitignore
      • gitignore node --force          → ⚠️  Overwrites .gitignore completely
      • gitignore rust --stdout         → 📄 Prints to stdout
      • gitignore java --dry-run        → 👁️  Preview changes

  gitignore update
    - ⬆️  Re-download the script from server
    - Ensures latest version
    - Self-replaces with new version

  gitignore version
    - ℹ️  Show script version and server URL

  gitignore help
    - ❓ Show usage information with emoji formatting
    - Display server URL and examples

  gitignore completion
    - 📦 Show shell completion installation instructions
    - Display commands for bash, zsh, fish
    - Links to completion script endpoints

Output Formatting:
  - Uses emojis for visual feedback:
    ✅ Success messages
    ⚠️  Warnings
    ❌ Errors
    🔍 Search results
    📋 Lists
    🎯 Actions performed
    ℹ️  Information
  - Color-coded where terminal supports it
  - Progress indicators for operations
```

### Installation Examples

```bash
# Method 1: Use web form at /cli (RECOMMENDED)
# - Visit https://yourserver.com/cli
# - Select your default templates (e.g., go, linux, vscode)
# - Click "Generate Script"
# - Copy the install command shown

# Method 2: Manual with custom defaults
# POSIX (Linux/macOS/BSD)
curl 'https://yourserver.com/api/v1/cli/sh?defaults=go,linux,vscode' > ~/.local/bin/gitignore
chmod +x ~/.local/bin/gitignore

# Or system-wide (requires sudo)
sudo curl 'https://yourserver.com/api/v1/cli/sh?defaults=go,macos,vscode' -o /usr/local/bin/gitignore
sudo chmod +x /usr/local/bin/gitignore

# PowerShell (Windows)
Invoke-WebRequest 'https://yourserver.com/api/v1/cli/ps?defaults=go,windows,vscode' -OutFile gitignore.ps1

# Add to PATH or create alias in PowerShell profile:
# Add-Content $PROFILE "`nSet-Alias gitignore $HOME\gitignore.ps1"

# Method 3: Default install (uses linux,macos,windows defaults)
curl https://yourserver.com/api/v1/cli/sh > ~/.local/bin/gitignore
chmod +x ~/.local/bin/gitignore

# Optional: Change defaults later by editing script
# POSIX
vim /usr/local/bin/gitignore
# Find the DEFAULT_TEMPLATES_START section and edit:
# DEFAULT_TEMPLATES_START
# go,node,docker
# vscode
# DEFAULT_TEMPLATES_END

# Windows (PowerShell)
notepad gitignore.ps1
# Find and edit the DEFAULT_TEMPLATES section
```

### Usage Examples

```bash
# 📋 List all templates
gitignore list

# 🔍 Search for templates
gitignore search python
gitignore search visual

# ✨ DEFAULT ACTION: Update/create .gitignore file with smart merge
# (automatically detects git root or uses current directory)
gitignore go
# 🎯 Detected git repo at: /path/to/repo
# ✅ Created .gitignore with template: go

# Run again with more templates - smart merge deduplicates!
gitignore vscode macos
# 🎯 Found existing CLI section
# ✨ Smart merge: adding vscode, macos (keeping go)
# ✅ Updated .gitignore with templates: go, vscode, macos (no duplicates!)

# Comma-separated templates
gitignore python,django
# ✨ Smart merge: adding python, django
# ✅ Updated .gitignore with templates: go, vscode, macos, python, django

# Running the same command again - no duplicates!
gitignore go vscode
# ℹ️  Templates go, vscode already applied
# ✅ No changes needed - .gitignore is up to date

# Mixed comma and space separation
gitignore rust,node linux,windows
# ✨ Smart merge: adding rust, node, linux, windows
# ✅ Updated .gitignore (deduplicated patterns)

# Use defaults embedded in script
gitignore
# ℹ️  Using default templates from script: linux,macos,windows
# ✨ Smart merge with existing templates
# ✅ Updated .gitignore

# ⚠️  Force overwrite (destructive)
gitignore node --force
# ⚠️  Overwritten .gitignore with node template

# 📄 Print to stdout instead of file
gitignore rust --stdout

# 👁️  Preview without modifying
gitignore java --dry-run
# 👁️  Would update: /path/to/repo/.gitignore
# 📋 Templates: java

# ⬆️  Update script
gitignore update
# ⬆️  Downloading latest version...
# ✅ Updated successfully!

# ℹ️  Show version
gitignore version
# gitignore v1.0.0
# Server: https://yourserver.com

# Git repo detection examples:
cd /path/to/repo/src/subfolder
gitignore go
# 🎯 Detected git repo at: /path/to/repo
# ✅ Updated .gitignore in /path/to/repo/.gitignore

cd /tmp/random-folder
gitignore python
# ℹ️  Not in a git repo, using current directory
# ✅ Created .gitignore in /tmp/random-folder/.gitignore
```

### Template Argument Parsing

```yaml
Supported Formats:
  - Space-separated: gitignore go python vscode
  - Comma-separated: gitignore go,python,vscode
  - Mixed: gitignore go,python vscode linux,macos
  - No arguments: gitignore (uses defaults from /usr/local/bin/.gitignore)

Parsing Logic:
  1. Collect all non-flag arguments
  2. Split each argument by comma
  3. Flatten into single array
  4. Remove empty strings and whitespace
  5. Remove duplicates
  6. Join with comma for API call

  Example (POSIX):
    templates=""
    for arg in "$@"; do
      # Skip flags
      case "$arg" in
        -*) continue ;;
      esac
      # Split by comma and append
      templates="$templates,$arg"
    done
    # Clean up: remove leading comma, split by comma, dedupe
    templates=$(echo "$templates" | tr ',' '\n' | grep -v '^$' | sort -u | tr '\n' ',')

Default Templates Embedded in Script:
  Location: Reads from the script file itself
    - POSIX: /usr/local/bin/gitignore (the script reads itself)
    - Windows: Where gitignore.ps1 is installed

  Embedded Format (inside script):
    # Special marker in script file
    # DEFAULT_TEMPLATES_START
    macos,linux,windows
    vim,vscode
    # DEFAULT_TEMPLATES_END

  How it works:
    1. Script reads its own file path ($0 in bash, $PSCommandPath in PowerShell)
    2. Extracts content between DEFAULT_TEMPLATES_START and DEFAULT_TEMPLATES_END
    3. Parses extracted lines (strip comments, split by comma/newline)
    4. Uses as default templates when no args provided

  Implementation (POSIX):
    read_default_templates() {
      script_path="$0"
      # Extract lines between markers
      sed -n '/# DEFAULT_TEMPLATES_START/,/# DEFAULT_TEMPLATES_END/p' "$script_path" \
        | grep -v '^#' \
        | tr '\n' ',' \
        | sed 's/,,*/,/g' \
        | sed 's/^,//;s/,$//'
    }

  Implementation (PowerShell):
    function Read-DefaultTemplates {
      $scriptPath = $PSCommandPath
      $content = Get-Content $scriptPath
      $inBlock = $false
      $templates = @()
      foreach ($line in $content) {
        if ($line -match '# DEFAULT_TEMPLATES_START') { $inBlock = $true; continue }
        if ($line -match '# DEFAULT_TEMPLATES_END') { break }
        if ($inBlock -and $line -notmatch '^#' -and $line.Trim()) {
          $templates += $line.Trim()
        }
      }
      return ($templates -join ',')
    }

  User Customization:
    - Users can edit /usr/local/bin/gitignore directly
    - Modify the DEFAULT_TEMPLATES section
    - Changes persist across uses
    - No separate config file needed

  Example Script Content:
    #!/bin/sh
    # gitignore CLI script
    # Server: https://yourserver.com

    # DEFAULT_TEMPLATES_START
    # Edit these lines to customize your defaults
    # Used when running 'gitignore' with no arguments
    # Based on existing /usr/local/bin/gitignore defaults
    linux,macos,windows
    # DEFAULT_TEMPLATES_END

    # Built-in hardcoded templates (always included):
    # - ignoredirmessage (disable git reminder in prompt)
    # - OS files (.DS_Store, Thumbs.db, .directory, etc.)
    # - .gitcommit, .build_failed*, *.bak
    # - .no_push, .no_git, .installed
    # - *.rewrite.sh, *.refactor.sh

    # ... rest of script ...

  Priority:
    1. Explicit templates (command-line args)
    2. Embedded defaults in script (if no args)
    3. Error if no templates found in either

Example Scenarios:
  # Script has embedded defaults: macos,vim,vscode
  gitignore                  → Uses: macos, vim, vscode (from script)
  gitignore go               → Uses: go (ignores embedded defaults)
  gitignore go,python        → Uses: go, python
  gitignore go linux,macos   → Uses: go, linux, macos

  # User customizes by editing /usr/local/bin/gitignore:
  vim /usr/local/bin/gitignore
  # Change DEFAULT_TEMPLATES section to:
  # go,node,docker
  # vscode

  gitignore                  → Now uses: go, node, docker, vscode
```

### Git Repository Detection

```yaml
Detection Logic (POSIX):
  1. Check if .git exists in current directory
  2. If not, traverse up parent directories
  3. Look for .git directory in each parent
  4. Stop at filesystem root or when .git found
  5. If .git found: use that directory as root
  6. If not found: use current directory (pwd)

  Implementation (sh):
    find_git_root() {
      dir="$(pwd)"
      while [ "$dir" != "/" ]; do
        if [ -d "$dir/.git" ]; then
          echo "$dir"
          return 0
        fi
        dir="$(dirname "$dir")"
      done
      pwd  # Not in git repo, use current dir
    }

Detection Logic (PowerShell):
  1. Check if .git exists in current location
  2. If not, use Get-Item and Parent property
  3. Walk up directory tree
  4. Stop at drive root or when .git found
  5. If .git found: use that directory
  6. If not found: use current location (Get-Location)

  Implementation (ps1):
    function Find-GitRoot {
      $dir = Get-Location
      while ($dir) {
        if (Test-Path (Join-Path $dir ".git")) {
          return $dir
        }
        $dir = Split-Path $dir -Parent
      }
      return Get-Location  # Not in git repo
    }

File Operations:
  - Target file: <git_root>/.gitignore
  - Default mode: Append/update (safe)
  - Force mode: Overwrite (with --force/-f flag)
  - Creates file if doesn't exist
  - Preserves existing content unless --force used

Smart Merging (default behavior):
  1. Read existing .gitignore (if exists)
  2. Parse into sections/blocks by CLI markers
  3. Extract existing patterns into normalized set
  4. Fetch new template(s) from API
  5. Parse new patterns, normalize, and deduplicate
  6. Only append truly new rules
  7. Update CLI marker with new template list
  8. Write back to file

  Deduplication Logic:
    - Normalize patterns (trim whitespace, remove duplicates)
    - Track which templates are already applied
    - Skip patterns that already exist (exact match)
    - Handle wildcards intelligently (*.log vs *.log.* )
    - Preserve user-added custom rules
    - Update existing CLI section instead of creating new ones

  CLI Markers in .gitignore:
    # === gitignore CLI ===
    # Server: https://server.com
    # Generated: 2025-10-10T12:34:56Z
    # Templates: go, linux, vscode
    # Do not edit this section manually - managed by gitignore CLI

    [template content here]

    # === End gitignore CLI ===

  Update Behavior:
    - If markers exist → update that section only
    - Track templates in marker comment
    - Adding new templates → merge and update marker
    - Removing templates → regenerate section from remaining
    - Preserve everything outside markers

  Example - Initial Creation:
    gitignore go linux

    Creates:
    # === gitignore CLI ===
    # Templates: go, linux
    # Generated: 2025-10-10T12:34:56Z
    # Server: https://server.com

    # Go
    *.exe
    vendor/

    # Linux
    *~
    .directory

    # === End gitignore CLI ===

  Example - Adding Templates (Smart Merge):
    gitignore vscode

    Updates to:
    # === gitignore CLI ===
    # Templates: go, linux, vscode
    # Generated: 2025-10-10T14:22:10Z
    # Server: https://server.com

    # Go
    *.exe
    vendor/

    # Linux
    *~
    .directory

    # Visual Studio Code
    .vscode/
    *.code-workspace

    # === End gitignore CLI ===

  Example - With Existing Content:
    # Custom user rules
    .env
    secrets.json

    # === gitignore CLI ===
    # Templates: go, vscode
    ...
    # === End gitignore CLI ===

    # More custom rules
    local-config.yaml

  Deduplication Algorithm (POSIX):
    # Read existing .gitignore
    existing_patterns=()
    if [ -f .gitignore ]; then
      # Extract patterns from CLI section
      in_section=false
      while IFS= read -r line; do
        if [[ "$line" == "# === gitignore CLI ===" ]]; then
          in_section=true
          continue
        fi
        if [[ "$line" == "# === End gitignore CLI ===" ]]; then
          in_section=false
          continue
        fi
        if [ "$in_section" = true ] && [[ ! "$line" =~ ^# ]] && [ -n "$line" ]; then
          existing_patterns+=("$line")
        fi
      done < .gitignore
    fi

    # Fetch new templates
    new_content=$(curl -LSs "$SERVER/api/v1/combine?templates=$templates")

    # Deduplicate
    final_patterns=()
    while IFS= read -r line; do
      # Skip comments and empty lines for dedup check
      if [[ "$line" =~ ^# ]] || [ -z "$line" ]; then
        final_patterns+=("$line")
        continue
      fi

      # Check if pattern already exists
      pattern_exists=false
      for existing in "${existing_patterns[@]}"; do
        if [ "$line" = "$existing" ]; then
          pattern_exists=true
          break
        fi
      done

      if [ "$pattern_exists" = false ]; then
        final_patterns+=("$line")
      fi
    done <<< "$new_content"

  Force Mode (--force):
    - Replaces entire .gitignore file
    - No deduplication (starts fresh)
    - Removes all existing content including user rules
    - Shows warning before proceeding
```

### Shell Completion Scripts

```yaml
Purpose:
  Enable tab completion for the gitignore CLI command in shells

Completion Features:
  - Command completion: list, search, update, version, help, completion
  - Template name completion: Dynamically generated from /api/v1/list
  - Flag completion: --force, --stdout, --dry-run, --append
  - Context-aware: Only show relevant completions based on command

Generation Process:
  1. Server receives request for completion script
  2. Fetches current template list from in-memory index
  3. Injects template names into completion script template
  4. Returns shell-specific completion script

Template Variables:
  {{.ServerURL}}     - Server URL for fetching template list
  {{.Templates}}     - Array of all template names
  {{.Commands}}      - Array of CLI commands
  {{.Flags}}         - Array of available flags

Bash Completion Example:
  _gitignore_completions() {
    local cur prev
    cur="${COMP_WORDS[COMP_CWORD]}"
    prev="${COMP_WORDS[COMP_CWORD-1]}"

    # Commands
    if [ $COMP_CWORD -eq 1 ]; then
      COMPREPLY=( $(compgen -W "list search update version help completion" -- "$cur") )
      return 0
    fi

    # Template names (from server)
    case "$prev" in
      search)
        COMPREPLY=( $(compgen -W "{{range .Templates}}{{.}} {{end}}" -- "$cur") )
        return 0
        ;;
    esac

    # Flags
    COMPREPLY=( $(compgen -W "--force --stdout --dry-run --append" -- "$cur") )
  }
  complete -F _gitignore_completions gitignore

Zsh Completion Example:
  #compdef gitignore

  _gitignore() {
    local -a commands templates flags
    commands=(
      'list:List all available templates'
      'search:Search templates by name'
      'update:Update the gitignore script'
      'version:Show version information'
      'help:Show help information'
      'completion:Show completion installation instructions'
    )
    templates=({{range .Templates}}'{{.}}' {{end}})
    flags=(
      '--force[Overwrite .gitignore completely]'
      '--stdout[Print to stdout instead of file]'
      '--dry-run[Preview changes without modifying]'
      '--append[Explicitly append (default)]'
    )

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

Fish Completion Example:
  # Commands
  complete -c gitignore -n "__fish_use_subcommand" -a "list" -d "List all templates"
  complete -c gitignore -n "__fish_use_subcommand" -a "search" -d "Search templates"
  complete -c gitignore -n "__fish_use_subcommand" -a "update" -d "Update script"
  complete -c gitignore -n "__fish_use_subcommand" -a "version" -d "Show version"
  complete -c gitignore -n "__fish_use_subcommand" -a "help" -d "Show help"
  complete -c gitignore -n "__fish_use_subcommand" -a "completion" -d "Show completion setup"

  # Template names
  {{range .Templates}}
  complete -c gitignore -a "{{.}}" -d "{{.}} template"
  {{end}}

  # Flags
  complete -c gitignore -l force -s f -d "Overwrite .gitignore completely"
  complete -c gitignore -l stdout -s o -d "Print to stdout"
  complete -c gitignore -l dry-run -s d -d "Preview changes"
  complete -c gitignore -l append -s a -d "Append to file"

Dynamic Updates:
  - Completion scripts can be regenerated to get latest templates
  - Run: curl https://server/api/v1/cli/completion/bash > ~/.local/share/bash-completion/completions/gitignore
  - Reload shell or source completion file
```

### Dynamic Server URL & Customization

```yaml
Template Variables (injected during generation):
  {{.ServerURL}}       - Server URL (e.g., https://yourserver.com)
  {{.DefaultTemplates}} - Comma-separated default templates
  {{.GeneratedDate}}   - ISO 8601 timestamp of generation

Script Generation Process:
  1. User visits /cli (web form) OR directly requests /api/v1/cli/sh or /api/v1/cli/ps
  2. User selects default templates (via form or ?defaults= param)
  3. Server reads template from embed.FS
  4. Server injects:
     - Current server URL
     - Selected default templates
     - Generation timestamp
  5. Returns rendered script ready to save
  6. User saves and uses immediately

Web Form Features (/cli):
  - Template search with autocomplete
  - Category filtering (Languages, IDEs, OS, Tools)
  - Popular templates suggested
  - Live preview of DEFAULT_TEMPLATES section
  - Platform tabs (Linux/macOS, Windows)
  - Copy install command button
  - Direct download button
  - QR code for remote installation

Query String Customization:
  /api/v1/cli/sh?defaults=go,python,vscode,linux
  /api/v1/cli/ps?defaults=csharp,windows,visualstudio

  - Accepts comma-separated template names
  - Case-insensitive matching
  - Invalid templates ignored with warning comment in script
  - Empty/missing defaults → uses hardcoded defaults (linux,macos,windows)

Benefits:
  - No manual configuration needed
  - Self-documenting (server URL embedded)
  - Customizable via web form or URL
  - Works with any domain/port
  - Self-updating via gitignore update command
  - Generated timestamp for tracking versions
```

---

## 🔐 Authentication

### Overview

This project uses **admin-only authentication** - all .gitignore templates are public, only server configuration requires authentication.

**Complete guide**: [SERVER.md](./docs/SERVER.md)

### Authentication Methods

```yaml
1. API Token (Bearer):
   Header: Authorization: Bearer <token>
   Use: Programmatic access to admin API
   Format: 64-character hex string
   Routes: /api/v1/admin/*

2. Basic Auth:
   Header: Authorization: Basic <base64(user:pass)>
   Use: Web UI access
   Browser: Prompts automatically
   Routes: /admin/*
```

### First Run Setup

```yaml
On first startup:
  1. Check if admin credentials exist in database

  2. If not, generate:
     - Username: $ADMIN_USER or "administrator"
     - Password: $ADMIN_PASSWORD or random 16-char
     - Token: $ADMIN_TOKEN or random 64-char hex

  3. Save to database (SHA-256 hashed)

  4. Write to {CONFIG_DIR}/admin_credentials (0600)
     Example: /etc/gitignore/admin_credentials

  5. Display credentials in console output
     ⚠️  Shown once - save securely!

Credential File Format:
  GITIGNORE API - ADMIN CREDENTIALS
  ========================================
  WEB UI LOGIN:
    URL:      http://server:port/admin
    Username: administrator
    Password: <password>

  API TOKEN:
    Header:   Authorization: Bearer <token>
    Token:    <64-char-hex>

  Created: 2024-01-01 12:00:00
  ========================================
```

### Environment Variables

```yaml
First Run Only (ignored after setup):
  ADMIN_USER=admin            # Default: administrator
  ADMIN_PASSWORD=secure123    # Default: random 16-char
  ADMIN_TOKEN=abc123...       # Default: random 64-char hex

After first run:
  Credentials stored in database
  Environment variables ignored
  To reset: delete database
```

---

## 🗺️ Routes & Endpoints

### Route Matching Philosophy

**Routes must mirror between web and API:**
- `/` ↔ `/api/v1`
- `/search` ↔ `/api/v1/search`
- `/docs` ↔ `/api/v1/docs`
- `/admin` ↔ `/api/v1/admin`

### Public Routes (No Authentication)

```yaml
Homepage:
  GET  /                      → Home page with search interface
  GET  /api/v1                → API information JSON

Search:
  GET  /search                → Search page
  GET  /api/v1/search         → Search templates (JSON)
    Query params:
      ?q=query               - Search term
      ?category=name         - Filter by category
      ?limit=50             - Results limit
      ?offset=0             - Pagination

Template Details:
  GET  /template/:name        → Template detail page
  GET  /api/v1/template/:name → Template content (text/plain)
  GET  /api/v1/template/:name.json → Template metadata (JSON)

Combine Templates:
  GET  /combine               → Combine templates page
  GET  /api/v1/combine        → Combine multiple templates
    Query params:
      ?templates=go,python,vscode  - Comma-separated template names
      ?format=text                 - Output format (text, json)

Categories:
  GET  /categories            → Categories page
  GET  /api/v1/categories     → List all categories (JSON)
  GET  /api/v1/category/:name → Templates in category (JSON)

List All:
  GET  /list                  → List all templates page
  GET  /api/v1/list           → List all templates (JSON)

Statistics:
  GET  /stats                 → Stats page
  GET  /api/v1/stats          → Template statistics (JSON)

Export:
  GET  /api/v1/templates.json → Full template list JSON
  GET  /api/v1/templates.tar.gz → All templates as tar.gz

Documentation:
  GET  /docs                  → API documentation page
  GET  /api/v1/docs           → Swagger UI (interactive API docs)
  GET  /api/v1/openapi.json   → OpenAPI 3.0 specification (JSON)
  GET  /api/v1/openapi.yaml   → OpenAPI 3.0 specification (YAML)

GraphQL:
  POST /api/v1/graphql        → GraphQL endpoint
  GET  /graphiql              → GraphiQL playground (interactive)
  GET  /api/v1/schema.graphql → GraphQL schema definition

CLI Scripts:
  GET  /cli                      → CLI customization page (web form to customize script)
  GET  /api/v1/cli/sh            → POSIX-compliant shell script (bash/zsh/sh)
  GET  /api/v1/cli/ps            → PowerShell script (Windows)

  Shell Completion Scripts:
    GET  /api/v1/cli/completion/bash  → Bash completion script
    GET  /api/v1/cli/completion/zsh   → Zsh completion script
    GET  /api/v1/cli/completion/fish  → Fish completion script

    Features:
      • Tab completion for commands (list, search, update, version, help)
      • Tab completion for template names (dynamically generated from available templates)
      • Tab completion for flags (--force, --stdout, --dry-run, --append)
      • Context-aware suggestions
      • Works with installed 'gitignore' command

    Installation:
      # Bash
      curl https://server/api/v1/cli/completion/bash > /etc/bash_completion.d/gitignore
      # Or user-local:
      mkdir -p ~/.local/share/bash-completion/completions
      curl https://server/api/v1/cli/completion/bash > ~/.local/share/bash-completion/completions/gitignore

      # Zsh
      curl https://server/api/v1/cli/completion/zsh > /usr/local/share/zsh/site-functions/_gitignore
      # Or user-local:
      mkdir -p ~/.zsh/completion
      curl https://server/api/v1/cli/completion/zsh > ~/.zsh/completion/_gitignore
      # Add to .zshrc: fpath=(~/.zsh/completion $fpath)

      # Fish
      curl https://server/api/v1/cli/completion/fish > ~/.config/fish/completions/gitignore.fish

  CLI Customization Page (/cli):
    Interactive web form allowing users to:
      • Select default templates (multiselect with search)
      • Preview generated script
      • Download customized script with one click
      • Copy install command with embedded defaults
      • See installation instructions for their platform
      • Download shell completion scripts

    Features:
      - Template search/filter by category
      - Popular templates suggested
      - Preview default templates section
      - Platform-specific install commands
      - QR code for remote/mobile install
      - Links to completion script downloads

  Script Generation (/api/v1/cli/sh or /api/v1/cli/ps):
    Query Parameters:
      ?defaults=template1,template2,template3
        - Embeds specified templates as defaults
        - Example: /api/v1/cli/sh?defaults=go,linux,vscode

    Script Functionality:
      • gitignore list              - List all templates
      • gitignore search <query>    - Search templates
      • gitignore <template...>     - Generate/update .gitignore
      • gitignore update            - Update the script itself
      • gitignore completion        - Show completion installation instructions

  Installation Examples:
    # POSIX with custom defaults (Linux/macOS/BSD)
    curl 'https://server/api/v1/cli/sh?defaults=go,linux,vscode' > ~/.local/bin/gitignore && chmod +x ~/.local/bin/gitignore

    # PowerShell with custom defaults (Windows)
    Invoke-WebRequest 'https://server/api/v1/cli/ps?defaults=go,windows,vscode' -OutFile gitignore.ps1

    # Use web form at /cli to customize and get install command

    # Install shell completion
    curl https://server/api/v1/cli/completion/bash > ~/.local/share/bash-completion/completions/gitignore

Health:
  GET  /healthz               → Health check (JSON)
  GET  /api/v1/healthz        → Health check (JSON)
  GET  /api/v1/healthz.txt    → Health check (plain text)

Static Assets:
  GET  /static/*              → CSS, JS, images (embedded)
  GET  /favicon.ico           → Favicon
  GET  /robots.txt            → Robots file
```

### Admin Routes (Authentication Required)

```yaml
Dashboard:
  GET  /admin                 → Admin dashboard (Basic Auth)
  GET  /api/v1/admin          → Admin info (Bearer Token)

Settings:
  GET  /admin/settings        → Settings page
  POST /admin/settings        → Update settings
  GET  /api/v1/admin/settings → Get all settings (JSON)
  PUT  /api/v1/admin/settings → Update settings (JSON)

Database:
  GET  /admin/database        → Database management page
  POST /admin/database/test   → Test connection
  GET  /api/v1/admin/database → Database status (JSON)
  POST /api/v1/admin/database/test → Test connection (JSON)

Logs:
  GET  /admin/logs            → Logs viewer page
  GET  /admin/logs/:type      → View specific log
  GET  /api/v1/admin/logs     → List available logs (JSON)
  GET  /api/v1/admin/logs/:type → Get log content (JSON)

Backup:
  GET  /admin/backup          → Backup management page
  POST /admin/backup/create   → Create backup
  POST /admin/backup/restore  → Restore backup
  GET  /api/v1/admin/backup   → List backups (JSON)
  POST /api/v1/admin/backup   → Create backup (JSON)
  DELETE /api/v1/admin/backup/:id → Delete backup

Health:
  GET  /admin/healthz         → Server health page
  GET  /api/v1/admin/healthz  → Detailed health (JSON)
```

### Response Format & Content Negotiation

```yaml
Content-Type Priority (for template endpoints):
  1. Check Accept header
  2. Check User-Agent for browser detection
  3. Default to text/plain

Response Types:

Plain Text (DEFAULT):
  Content-Type: text/plain
  Use: When no Accept header or Accept: text/plain
  Format: Raw .gitignore content
  Example:
    # Go
    *.exe
    *.o
    vendor/

JSON Response:
  Content-Type: application/json
  Use: When Accept: application/json
  Success:
    {
      "success": true,
      "data": { ... },
      "timestamp": "2024-01-01T12:00:00Z"
    }
  Error:
    {
      "success": false,
      "error": {
        "code": "INVALID_INPUT",
        "message": "Template not found",
        "field": "name"
      },
      "timestamp": "2024-01-01T12:00:00Z"
    }

HTML Response:
  Content-Type: text/html
  Use: When User-Agent contains Mozilla/Chrome/Safari (browser detection)
  Format: Full HTML page rendered with html/template

API Documentation Formats:
  GET /api/v1/docs         → Swagger UI (HTML)
  GET /api/v1/openapi.json → OpenAPI 3.0 specification (JSON)
  GET /api/v1/graphql      → GraphQL endpoint
  GET /graphiql            → GraphQL playground (HTML)

Template Combining:
  GET /api/v1/combine?templates=go,python,vscode
  - Default: text/plain (combined .gitignore content)
  - With Accept: application/json → metadata about combined templates
  - Browser: HTML page with preview

Examples:
  curl https://api/v1/template/go
  → text/plain response (raw .gitignore)

  curl -H "Accept: application/json" https://api/v1/template/go
  → JSON with metadata

  Browser: https://api/v1/template/go
  → HTML page with syntax highlighting
```

---

## ⚙️ Configuration

### Database Schema

```sql
-- Settings table
CREATE TABLE IF NOT EXISTS settings (
  key TEXT PRIMARY KEY,
  value TEXT NOT NULL,
  type TEXT NOT NULL CHECK (type IN ('string', 'number', 'boolean', 'json')),
  category TEXT NOT NULL,
  description TEXT,
  updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

-- Admin credentials table
CREATE TABLE IF NOT EXISTS admin_credentials (
  id INTEGER PRIMARY KEY CHECK (id = 1),
  username TEXT NOT NULL UNIQUE,
  password_hash TEXT NOT NULL,
  token_hash TEXT NOT NULL,
  created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
  updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
);
```

### Default Settings

```yaml
Server:
  server.title: "GitIgnore API"
  server.address: "0.0.0.0"
  server.http_port: 64000-64999 (random)
  server.https_enabled: false
  server.trust_proxy: true

URL Detection (Auto-populated):
  server.detected_proto: ""           # Auto-detected: "http" or "https"
  server.detected_host: ""            # Auto-detected: domain or IP
  server.detected_port: 0             # Auto-detected: port or 0 for standard
  server.last_proxy_update: ""        # Timestamp of last detection

Reverse Proxy:
  proxy.enabled: true
  proxy.trust_headers: true
  proxy.trusted_proxies: ["127.0.0.1", "::1", "10.0.0.0/8", "172.16.0.0/12", "192.168.0.0/16"]

Database:
  db.type: "sqlite"
  db.path: "{DATA_DIR}/gitignore.db"

Logging:
  log.level: "info"
  log.format: "json"
  log.access: true
```

### Modifying Settings

```yaml
Web UI:
  1. Navigate to /admin/settings
  2. Change values in form
  3. Click Save (applies immediately)

API:
  PUT /api/v1/admin/settings
  {
    "settings": {
      "server.title": "My GitIgnore API"
    }
  }

Environment (first run only):
  DATABASE_URL=sqlite:/data/gitignore.db
  PORT=8080
```

---

## 🎨 Frontend & UI

### Overview

The web frontend is built with Go `html/template` for server-side rendering, styled with the Dracula theme for a modern, dark aesthetic.

### Theme: Dracula

```yaml
Official Dracula Color Palette:
  Background:     #282a36
  Current Line:   #44475a
  Foreground:     #f8f8f2
  Comment:        #6272a4
  Cyan:           #8be9fd
  Green:          #50fa7b
  Orange:         #ffb86c
  Pink:           #ff79c6
  Purple:         #bd93f9
  Red:            #ff5555
  Yellow:         #f1fa8c

Applied To:
  - Page backgrounds: #282a36
  - Cards/panels: #44475a
  - Text: #f8f8f2
  - Links: #8be9fd (cyan)
  - Success: #50fa7b (green)
  - Warnings: #ffb86c (orange)
  - Errors: #ff5555 (red)
  - Buttons/CTAs: #bd93f9 (purple) or #ff79c6 (pink)
  - Code blocks: #f8f8f2 on #44475a
```

### UI Components

```yaml
Modern Components Used:

Modals:
  - Used for: confirmations, previews, forms
  - Backdrop blur effect
  - Dracula-themed with #44475a background
  - Close button (×) in top-right
  - Smooth fade-in/out animations
  - ESC key to close

  Examples:
    - Template preview modal
    - Confirm delete backup
    - View log content
    - Settings help/info

Toggles/Switches:
  - Used for: enable/disable settings
  - CSS-only toggle switches (no JS required)
  - Purple (#bd93f9) when enabled
  - Gray (#6272a4) when disabled
  - Smooth transition animations

  Examples:
    - Enable/disable HTTPS
    - Trust proxy headers
    - Enable access logging
    - Enable notifications

Buttons:
  - Primary: Purple (#bd93f9) background
  - Secondary: Gray (#44475a) background with purple border
  - Danger: Red (#ff5555) background
  - Success: Green (#50fa7b) background
  - Hover effects: Lighten by 10%
  - Active state: Darken by 10%
  - Rounded corners (border-radius: 6px)

  Examples:
    - Save Settings (primary)
    - Cancel (secondary)
    - Delete Backup (danger)
    - Create Backup (success)

Forms:
  - Input fields: #44475a background, #f8f8f2 text
  - Focus state: #bd93f9 border
  - Placeholder: #6272a4 (comment color)
  - Labels: #8be9fd (cyan)
  - Error messages: #ff5555 (red)
  - Success messages: #50fa7b (green)

Cards/Panels:
  - Background: #44475a
  - Border: 1px solid #6272a4
  - Border-radius: 8px
  - Box-shadow: 0 4px 6px rgba(0,0,0,0.3)
  - Padding: 1.5rem

  Used for:
    - Template cards
    - Admin dashboard widgets
    - Settings sections
    - Log viewers

Tables:
  - Header: #44475a background, #8be9fd text
  - Rows: Alternating #282a36 and #44475a
  - Hover: #6272a4 background
  - Borders: #6272a4
  - Responsive: Scroll on mobile

  Used for:
    - Template list
    - Backup history
    - Log entries
    - Admin statistics

Dropdowns/Selects:
  - Background: #44475a
  - Options hover: #6272a4
  - Border: #6272a4
  - Focus: #bd93f9 border
  - Custom arrow icon (purple)

  Used for:
    - Category filters
    - Database type selection
    - Log level selection

Search Bars:
  - Background: #44475a
  - Placeholder: #6272a4
  - Text: #f8f8f2
  - Focus: #bd93f9 border
  - Search icon: #8be9fd (cyan)
  - Clear button: #ff5555 (red)
  - Autocomplete dropdown: #44475a

Badges/Tags:
  - Category badges: #bd93f9 background
  - Status badges: Green/Orange/Red based on state
  - Count badges: #ff79c6 (pink)
  - Rounded pill shape

  Examples:
    - Template category (Languages, IDEs, etc.)
    - "Online" / "Offline" status
    - Template count

Progress Bars:
  - Track: #44475a
  - Fill: #bd93f9 gradient
  - Height: 8px
  - Border-radius: 4px
  - Animated fill

  Used for:
    - Upload progress
    - Backup creation
    - Template loading

Alerts/Notifications:
  - Info: #8be9fd background
  - Success: #50fa7b background
  - Warning: #ffb86c background
  - Error: #ff5555 background
  - Dismissible (× button)
  - Auto-dismiss after 5 seconds
  - Slide-in animation from top

  Examples:
    - "Settings saved successfully"
    - "Invalid template name"
    - "Backup created"
```

### Authentication UI

```yaml
Admin Login Page (/admin):
  - Centered card design
  - Dracula-themed form
  - Username and password fields
  - "Remember me" toggle
  - Login button (purple)
  - Error messages in red
  - Logo/title at top

Browser Basic Auth Integration:
  - Server sends: WWW-Authenticate: Basic realm="GitIgnore Admin"
  - Browser shows native auth dialog
  - Credentials stored in session
  - On success: redirect to /admin dashboard
  - On failure: 401 with error message

API Token Authentication:
  - No UI needed (header-based)
  - Token displayed in admin dashboard
  - Copy button next to token
  - Regenerate token button (with confirmation modal)
  - Token masked by default (click to reveal)

Session Management:
  - Logged in: Username displayed in navbar
  - Logout button in navbar
  - Session timeout configurable
  - "Session expired" modal with re-login prompt
```

### Layout Structure

```yaml
Base Template (base.html):
  - HTML5 structure
  - Dracula CSS loaded
  - Responsive meta tags
  - Font: Fira Code (monospace) or Inter (sans-serif)
  - Navbar (sticky top)
  - Main content area
  - Footer
  - Toast notification container

Navbar:
  - Background: #44475a
  - Logo/title: Left side
  - Navigation links: Center
  - Admin/Login: Right side
  - Mobile: Hamburger menu
  - Active page highlighted in purple

  Public Pages:
    - Home | Search | Browse | Docs | CLI

  Admin Pages:
    - Dashboard | Settings | Logs | Backups | Logout

Responsive Design:
  - Breakpoints:
    - Mobile: < 768px
    - Tablet: 768px - 1024px
    - Desktop: > 1024px
  - Mobile-first approach
  - Flexbox and CSS Grid
  - Touch-friendly buttons (min 44px)
```

### JavaScript (Minimal)

```yaml
Progressive Enhancement:
  - Core functionality works without JS
  - JS enhances UX (modals, live search, etc.)
  - No heavy frameworks (vanilla JS only)

Used For:
  - Modal open/close
  - Form validation (client-side)
  - Live search/filter
  - Copy to clipboard
  - Toggle password visibility
  - Autocomplete dropdowns
  - Toast notifications
  - Confirm dialogs
  - AJAX form submissions (optional)

Not Used For:
  - Page routing (server-side only)
  - Template rendering (html/template)
  - Authentication (server-side)
```

### Accessibility

```yaml
WCAG 2.1 AA Compliance:
  - Semantic HTML5 elements
  - ARIA labels where needed
  - Keyboard navigation support
  - Focus indicators (purple outline)
  - Alt text for images
  - Color contrast ratios met
  - Screen reader friendly

Keyboard Shortcuts:
  - / : Focus search
  - ESC: Close modals
  - Tab/Shift+Tab: Navigate
  - Enter: Submit forms
```

### Static Assets

```yaml
Directory: src/server/static/

CSS:
  - dracula.css           # Main theme
  - components.css        # UI components
  - layout.css            # Grid/flexbox layouts
  - animations.css        # Transitions/animations
  - responsive.css        # Media queries

JavaScript:
  - main.js               # Core functionality
  - modals.js             # Modal logic
  - search.js             # Live search
  - utils.js              # Helper functions

Images:
  - logo.svg              # GitIgnore logo
  - favicon.ico           # Browser icon
  - icons/*.svg           # UI icons (Dracula-colored)

Fonts (embedded or CDN):
  - Fira Code             # Monospace (code blocks)
  - Inter                 # Sans-serif (UI text)
```

### Example Pages

```yaml
Home Page (/):
  - Hero section with search bar
  - Popular templates grid (cards)
  - Quick links to categories
  - CLI installation instructions
  - Footer with links

Search Page (/search):
  - Search bar with live results
  - Category filters (dropdowns)
  - Template cards with preview
  - Pagination
  - Empty state message

Template Detail (/template/:name):
  - Template name and description
  - Category badges
  - Preview code block (syntax highlighted)
  - Download button
  - Copy to clipboard button
  - Related templates

Admin Dashboard (/admin):
  - Welcome message with username
  - Statistics cards (template count, API calls, etc.)
  - Quick actions (view logs, create backup)
  - System health indicators
  - Recent activity log

Admin Settings (/admin/settings):
  - Grouped sections (Server, Database, Logging, etc.)
  - Toggle switches for boolean settings
  - Input fields for text settings
  - Save/Cancel buttons
  - Reset to defaults button (with confirmation)
  - Help tooltips (? icons with modals)
```

---

## 🌐 URL Detection & Server Address

### Philosophy

**IMPORTANT**: The server NEVER shows or uses `localhost` in user-facing outputs (CLI scripts, URLs, etc.). Instead, it always attempts to detect and use the actual public-facing URL through:

1. **Reverse Proxy Headers** (X-Forwarded-Host, X-Forwarded-Proto, etc.)
2. **FQDN** (Fully Qualified Domain Name)
3. **Public IP Address** (if no domain available)
4. **Hostname** (from system)

This ensures that generated CLI scripts, links, and URLs always point to the actual accessible server address, not internal addresses.

### URL Detection Priority

```yaml
Server URL Detection (in order):
  1. X-Forwarded-Host + X-Forwarded-Proto (reverse proxy)
     - Example: https://gitignore.example.com
     - Most reliable for production deployments

  2. Host header + X-Forwarded-Proto
     - Example: https://api.company.com
     - Used when reverse proxy sets protocol but not host

  3. Public IP + detected protocol
     - Example: https://203.0.113.50:8080
     - Used when no domain/hostname available
     - Queries external service for public IP

  4. Hostname + detected protocol
     - Example: https://server01.local:8080
     - Last resort: uses system hostname

  5. NEVER use localhost or 127.0.0.1
     - Even in development, use actual network address
     - Use hostname or LAN IP instead

Detection Logic:
  1. Check reverse proxy headers (X-Forwarded-*)
  2. If behind proxy: use forwarded values
  3. If not: detect public IP via external service
  4. If IP detection fails: use system hostname
  5. If all else fails: use network interface IP
  6. Build complete URL: {protocol}://{host}:{port}

Protocol Detection:
  1. X-Forwarded-Proto (https/http)
  2. TLS connection status
  3. Default to http for development, https for production

Port Handling:
  - Omit standard ports (80 for http, 443 for https)
  - Include non-standard ports in URL
  - Example: https://example.com (port 443 omitted)
  - Example: https://example.com:8443 (port included)
```

### Database Persistence

```yaml
Reverse Proxy Detection & Persistence:
  When reverse proxy headers are detected:
    1. Extract protocol (X-Forwarded-Proto)
    2. Extract host (X-Forwarded-Host)
    3. Save to database settings:
       - server.detected_proto: "https"
       - server.detected_host: "gitignore.example.com"
       - server.last_proxy_update: timestamp
    4. Use these values for all URL generation
    5. Update on every request if values change

  Benefits:
    - Survives server restarts
    - No need to re-detect on startup
    - Consistent URLs across all endpoints
    - Falls back to detection if DB empty

  Settings Schema:
    server.detected_proto:
      - Type: string
      - Values: "http" or "https"
      - Updated: On every request with X-Forwarded-Proto
      - Default: "" (empty, triggers detection)

    server.detected_host:
      - Type: string
      - Values: domain or IP (e.g., "gitignore.example.com")
      - Updated: On every request with X-Forwarded-Host
      - Default: "" (empty, triggers detection)

    server.detected_port:
      - Type: number
      - Values: port number or 0 for standard ports
      - Updated: On every request with X-Forwarded-Port
      - Default: 0 (use standard port)

    server.last_proxy_update:
      - Type: timestamp
      - Values: ISO 8601 timestamp
      - Updated: On every successful proxy header detection
      - Used: For monitoring/debugging

  Update Logic:
    On every HTTP request:
      1. Check for reverse proxy headers
      2. If found AND different from DB values:
         - Update server.detected_proto
         - Update server.detected_host
         - Update server.detected_port
         - Update server.last_proxy_update timestamp
      3. Use DB values for URL generation
      4. If no proxy headers: fall back to detection methods

  Fallback Chain:
    1. Check database (server.detected_host)
    2. If empty: check request headers (X-Forwarded-*)
    3. If no headers: detect public IP
    4. If detection fails: use hostname
    5. Save successful result to database

Implementation:
  func (s *Server) detectServerURL(r *http.Request) string {
      // Check for reverse proxy headers
      if proto := r.Header.Get("X-Forwarded-Proto"); proto != "" {
          if host := r.Header.Get("X-Forwarded-Host"); host != "" {
              port := r.Header.Get("X-Forwarded-Port")

              // Save to database for persistence
              s.saveDetectedURL(proto, host, port)

              return buildURL(proto, host, port)
          }
      }

      // Fall back to database values
      if url := s.getDetectedURLFromDB(); url != "" {
          return url
      }

      // Fall back to public IP detection
      if ip := detectPublicIP(); ip != "" {
          proto := s.detectProtocol(r)
          port := s.config.Port
          url := buildURL(proto, ip, port)

          // Save to database
          s.saveDetectedURL(proto, ip, port)

          return url
      }

      // Last resort: hostname
      hostname := getHostname()
      proto := s.detectProtocol(r)
      port := s.config.Port
      url := buildURL(proto, hostname, port)

      // Save to database
      s.saveDetectedURL(proto, hostname, port)

      return url
  }

  func (s *Server) saveDetectedURL(proto, host, port string) {
      // Only update if changed
      if s.db.GetSetting("server.detected_proto") != proto {
          s.db.SetSetting("server.detected_proto", proto)
      }
      if s.db.GetSetting("server.detected_host") != host {
          s.db.SetSetting("server.detected_host", host)
      }
      if port != "" && s.db.GetSetting("server.detected_port") != port {
          s.db.SetSetting("server.detected_port", port)
      }
      s.db.SetSetting("server.last_proxy_update", time.Now().Format(time.RFC3339))
  }
```

### Public IP Detection

```yaml
Methods (tried in order):
  1. Query ipify.org API
  2. Query ifconfig.me
  3. Query api.ipify.org
  4. Parse from DNS resolution
  5. Fallback to hostname

Implementation:
  func detectPublicIP() string {
      // Try multiple services
      services := []string{
          "https://api.ipify.org",
          "https://ifconfig.me/ip",
          "https://api.ip.sb/ip",
      }

      for _, service := range services {
          if ip := queryService(service); ip != "" {
              return ip
          }
      }

      // Fallback to hostname
      return getHostname()
  }

Caching:
  - Cache detected public IP for 5 minutes
  - Refresh on request if cache expired
  - Avoid excessive external queries
  - Successful detections saved to database
```

### Use Cases

```yaml
CLI Script Generation:
  - Generates scripts with actual server URL
  - Never embeds localhost or 127.0.0.1
  - Users can install and use immediately

  Bad:  SERVER_URL="http://localhost:8080"
  Good: SERVER_URL="https://gitignore.example.com"
  Good: SERVER_URL="https://203.0.113.50:8080"
  Good: SERVER_URL="https://server01.company.local"

Admin Credentials File:
  - Shows actual access URL
  - Users know how to reach the server

  Bad:  URL: http://localhost:64080/admin
  Good: URL: https://gitignore.example.com/admin
  Good: URL: https://10.0.1.50:64080/admin

API Documentation:
  - Example URLs use detected server address
  - Copy-paste ready for users

  Bad:  curl http://localhost/api/v1/list
  Good: curl https://gitignore.example.com/api/v1/list

Links in Responses:
  - Self-referencing URLs use detected address
  - Useful for API discovery
```

### Development Mode Exception

```yaml
Development Mode (--dev):
  - Still attempts to detect real URL
  - Uses LAN IP if available (e.g., 192.168.1.100)
  - Uses hostname as fallback
  - NEVER uses localhost even in dev mode

  Why:
    - CLI scripts should work from other machines
    - Testing from mobile devices
    - Sharing with team members
    - Consistent behavior across environments
```

---

## 🔀 Reverse Proxy Support

### Overview

The server automatically detects and respects reverse proxy headers when deployed behind nginx, Caddy, Apache, Traefik, or other reverse proxies. This is the PRIMARY method for URL detection in production.

### Supported Headers

```yaml
Standard Headers (automatically detected and used):

X-Forwarded-For:
  - Original client IP address
  - Used for logging and rate limiting
  - Example: X-Forwarded-For: 203.0.113.195, 198.51.100.178

X-Forwarded-Proto:
  - Original protocol (http/https)
  - Used for generating absolute URLs
  - Example: X-Forwarded-Proto: https

X-Forwarded-Host:
  - Original host header
  - Used for generating URLs in responses
  - Example: X-Forwarded-Host: gitignore.example.com

X-Forwarded-Port:
  - Original port
  - Used for URL generation
  - Example: X-Forwarded-Port: 443

X-Real-IP:
  - Real client IP (alternative to X-Forwarded-For)
  - Example: X-Real-IP: 203.0.113.195

Forwarded (RFC 7239):
  - Modern standard header
  - Example: Forwarded: for=203.0.113.195;proto=https;host=gitignore.example.com

CF-Connecting-IP:
  - Cloudflare original IP
  - Trusted when request comes from Cloudflare
  - Example: CF-Connecting-IP: 203.0.113.195

True-Client-IP:
  - Akamai/Cloudflare true client IP
  - Example: True-Client-IP: 203.0.113.195
```

### Header Priority

```yaml
Client IP Detection (in order):
  1. CF-Connecting-IP (if from Cloudflare)
  2. True-Client-IP (if from Akamai/Cloudflare)
  3. X-Real-IP
  4. X-Forwarded-For (first IP in list)
  5. Forwarded (for= field)
  6. Direct connection IP

Protocol Detection:
  1. X-Forwarded-Proto
  2. Forwarded (proto= field)
  3. Direct connection scheme

Host Detection:
  1. X-Forwarded-Host
  2. Forwarded (host= field)
  3. Host header
```

### Security

```yaml
Trusted Proxies:
  Default trusted networks:
    - 127.0.0.0/8 (localhost IPv4)
    - ::1 (localhost IPv6)
    - 10.0.0.0/8 (private)
    - 172.16.0.0/12 (private)
    - 192.168.0.0/16 (private)

  Only trust headers from trusted proxy IPs
  Configurable via admin settings

Header Validation:
  - Validate IP addresses
  - Validate protocols (http/https only)
  - Validate hostnames
  - Reject malformed headers
  - Log suspicious headers

Configuration:
  proxy.enabled: true                    # Enable proxy support
  proxy.trust_headers: true              # Trust proxy headers
  proxy.trusted_proxies: [...]           # List of trusted proxy IPs/CIDRs
```

### Use Cases

```yaml
CLI Script Generation:
  - Detects actual public URL from X-Forwarded-* headers
  - Generates scripts with correct server URL
  - Example: User behind nginx sees https://gitignore.example.com in scripts

Access Logging:
  - Logs real client IP from X-Forwarded-For
  - Not proxy IP (127.0.0.1)
  - Useful for analytics and security

Rate Limiting:
  - Uses real client IP for rate limits
  - Prevents proxy IP from being rate limited
  - Fair per-client limiting

HTTPS Detection:
  - Detects X-Forwarded-Proto: https
  - Generates https:// URLs in responses
  - Redirects or enforces HTTPS if configured
```

### Configuration Examples

```yaml
Admin Settings:
  # Enable reverse proxy support
  proxy.enabled: true
  proxy.trust_headers: true

  # Trust specific proxy IPs
  proxy.trusted_proxies: ["10.0.1.50", "10.0.1.51"]

  # Trust entire subnet
  proxy.trusted_proxies: ["10.0.0.0/8"]

  # Disable for direct connections only
  proxy.enabled: false
```

### Nginx Configuration Example

```nginx
server {
    listen 443 ssl http2;
    server_name gitignore.example.com;

    ssl_certificate /path/to/cert.pem;
    ssl_certificate_key /path/to/key.pem;

    location / {
        proxy_pass http://127.0.0.1:64080;

        # Forward original client info
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
        proxy_set_header X-Forwarded-Host $host;
        proxy_set_header X-Forwarded-Port $server_port;

        # WebSocket support (for future use)
        proxy_http_version 1.1;
        proxy_set_header Upgrade $http_upgrade;
        proxy_set_header Connection "upgrade";
    }
}
```

### Caddy Configuration Example

```caddy
gitignore.example.com {
    reverse_proxy localhost:64080

    # Caddy automatically sets:
    # - X-Forwarded-For
    # - X-Forwarded-Proto
    # - X-Forwarded-Host
}
```

### Docker with Traefik Example

```yaml
services:
  gitignore:
    image: gitignore:latest
    labels:
      - "traefik.enable=true"
      - "traefik.http.routers.gitignore.rule=Host(`gitignore.example.com`)"
      - "traefik.http.routers.gitignore.tls=true"
      - "traefik.http.services.gitignore.loadbalancer.server.port=80"
    environment:
      - proxy.enabled=true
      - proxy.trust_headers=true
      # Traefik automatically adds X-Forwarded-* headers
```

### Testing Proxy Headers

```bash
# Test with X-Forwarded-For
curl -H "X-Forwarded-For: 203.0.113.195" http://localhost:64080/api/v1/cli/sh

# Should generate script with public URL if behind proxy
curl -H "X-Forwarded-Host: gitignore.example.com" \
     -H "X-Forwarded-Proto: https" \
     http://localhost:64080/api/v1/cli/sh

# Generated script should contain:
# SERVER_URL="https://gitignore.example.com"
```

---

## 🔨 Build & Deployment

### Makefile Targets

```makefile
Targets:
  make deps              # Download Go dependencies
  make build             # Build all platforms
  make test              # Run tests
  make run               # Build and run (current platform)
  make docker            # Build Docker image
  make release           # Create GitHub release
  make clean             # Remove build artifacts

Build Process:
  1. go mod download
  2. go build for all platforms:
     - Linux: amd64, arm64
     - Windows: amd64, arm64
     - macOS: amd64, arm64 (Apple Silicon)
     - FreeBSD: amd64
  3. Output binaries to ./binaries/ directory
  4. Create release artifacts in ./release/ directory
  5. Auto-increment version

Directory Structure:
  ./binaries/                    # Built binaries (gitignored)
    ├── gitignore-linux-amd64
    ├── gitignore-linux-arm64
    ├── gitignore-windows-amd64.exe
    ├── gitignore-windows-arm64.exe
    ├── gitignore-macos-amd64
    ├── gitignore-macos-arm64
    ├── gitignore-bsd-amd64
    └── gitignore                # Current platform symlink

  ./release/                     # Release artifacts (gitignored)
    ├── gitignore-v1.0.0-linux-amd64.tar.gz
    ├── gitignore-v1.0.0-linux-arm64.tar.gz
    ├── gitignore-v1.0.0-windows-amd64.zip
    ├── gitignore-v1.0.0-windows-arm64.zip
    ├── gitignore-v1.0.0-macos-amd64.tar.gz
    ├── gitignore-v1.0.0-macos-arm64.tar.gz
    ├── gitignore-v1.0.0-bsd-amd64.tar.gz
    ├── checksums.txt            # SHA256 checksums
    └── release-notes.md         # Release notes

  ./scripts/                     # Production scripts (self-contained)
    ├── install.sh               # Installation script
    └── backup.sh                # Backup script

  ./tests/                       # Development & test scripts
    ├── test-docker.sh           # Docker testing
    ├── test-api.sh              # API testing
    └── benchmark.sh             # Performance benchmarks
```

### Docker

```yaml
Dockerfile:
  Multi-stage build (Go builder → scratch runtime)
  CGO_ENABLED=0 for static binary
  Health check: /healthz endpoint via --status flag
  Volumes: /config, /data, /logs
  User: 65534:65534 (nobody)
  Exposed port: 8080

Building:
  docker build -t gitignore:latest .

  With version:
    docker build \
      --build-arg VERSION=1.0.0 \
      --build-arg COMMIT=$(git rev-parse --short HEAD) \
      --build-arg BUILD_DATE=$(date -u +%Y-%m-%dT%H:%M:%SZ) \
      -t gitignore:1.0.0 .

Production Deployment:
  Uses docker-compose.yml with ./rootfs for persistent storage

  Start:
    docker-compose up -d

  Volumes mounted to ./rootfs:
    - ./rootfs/config → /config (in container)
    - ./rootfs/data → /data (in container)
    - ./rootfs/logs → /logs (in container)

  Default configuration:
    - Internal port: 80 (Docker)
    - External port: 64080 (65xxx range)
    - Localhost only: 127.0.0.1:64080:80

  For public access:
    Change port mapping in docker-compose.yml:
      - "64080:80"      # Public HTTP

  Access:
    http://localhost:64080            # Homepage
    http://localhost:64080/admin      # Admin UI (Basic Auth)
    http://localhost:64080/api/v1     # API endpoints

  Check credentials:
    cat ./rootfs/config/admin_credentials

  View logs:
    docker-compose logs -f gitignore
    cat ./rootfs/logs/access.log

  Set admin credentials (first run):
    Edit docker-compose.yml environment:
      - ADMIN_USER=administrator
      - ADMIN_PASSWORD=strong-password

  Stop:
    docker-compose down

Testing/Debugging:
  Uses docker-compose.test.yml with /tmp for ephemeral storage

  Test (from project root):
    ./tests/test-docker.sh

  Or manually:
    docker-compose -f docker-compose.test.yml up -d

  Volumes in /tmp/gitignore/rootfs (automatically cleaned):
    - /tmp/gitignore/rootfs/config → /config
    - /tmp/gitignore/rootfs/data → /data
    - /tmp/gitignore/rootfs/logs → /logs

  Access:
    http://localhost:8080             # Test server

  Cleanup:
    docker-compose -f docker-compose.test.yml down
    sudo rm -rf /tmp/gitignore/rootfs

Docker Run (Manual):
  # Production (with ./rootfs)
  docker run -d \
    --name gitignore \
    -p 127.0.0.1:64080:80 \
    -v $(pwd)/rootfs/config:/config \
    -v $(pwd)/rootfs/data:/data \
    -v $(pwd)/rootfs/logs:/logs \
    -e PORT=80 \
    -e ADMIN_PASSWORD=changeme \
    --restart unless-stopped \
    gitignore:latest

  # Testing (with /tmp)
  docker run -d \
    --name gitignore-test \
    -p 127.0.0.1:8080:80 \
    -v /tmp/gitignore/rootfs/config:/config \
    -v /tmp/gitignore/rootfs/data:/data \
    -v /tmp/gitignore/rootfs/logs:/logs \
    -e PORT=80 \
    -e ADMIN_PASSWORD=testpass \
    gitignore:latest

External Database (PostgreSQL):
  docker-compose.yml includes optional postgres service

  1. Uncomment postgres section
  2. Set environment on gitignore service:
     - DATABASE_URL=postgres://gitignore:changeme@postgres:5432/gitignore

  3. Start both:
     docker-compose up -d

  Database is mounted to /tmp/gitignore/rootfs/db/postgres for testing
```

### Manual Installation

```bash
# Download binary
wget https://github.com/apimgr/gitignore/releases/latest/download/gitignore-linux-amd64
chmod +x gitignore-linux-amd64
sudo mv gitignore-linux-amd64 /usr/local/bin/gitignore

# Create directories (as root)
sudo mkdir -p /etc/gitignore /var/lib/gitignore /var/log/gitignore

# Run
sudo gitignore --port 80

# Or run as user (random port)
gitignore
# Check output for port and credentials
```

### Production Scripts

```yaml
Location: ./scripts/

Self-Contained Scripts:
  All production scripts in ./scripts/ are fully self-contained and portable.
  They have no external dependencies and can be used independently.

install.sh:
  - Interactive installer for the gitignore binary
  - Detects OS and downloads appropriate binary
  - Sets up directories and permissions
  - Creates systemd/launchd service files
  - Configures initial admin credentials
  - Self-contained: Works on any POSIX system

backup.sh:
  - Database backup script
  - Supports scheduled backups (cron/systemd timer)
  - Rotation policy (keep last N backups)
  - Compression (gzip/bzip2)
  - Self-contained: No dependencies

Usage:
  # Installation
  curl https://raw.githubusercontent.com/apimgr/gitignore/main/scripts/install.sh | bash

  # Or download and run
  wget https://raw.githubusercontent.com/apimgr/gitignore/main/scripts/install.sh
  chmod +x install.sh
  ./install.sh

  # Backup
  ./scripts/backup.sh --config /etc/gitignore --output /backups
```

### Development Scripts

```yaml
Location: ./tests/

Development & Testing Scripts:
  All development and testing scripts are in ./tests/ directory.
  These are used during development, testing, and CI/CD.

test-docker.sh:
  - Tests Docker build process
  - Validates Docker image
  - Multi-stage build verification
  - Image size checks
  - Cleanup after tests

test-incus.sh:
  - Creates Incus test container
  - Deploys binary to container
  - Tests in isolated environment
  - Validates API endpoints
  - Verifies admin authentication
  - Network testing
  - Cleanup after tests

test-api.sh:
  - API endpoint testing
  - Response validation
  - Performance checks
  - Error handling tests

test-cli.sh:
  - CLI script testing
  - Template combination tests
  - Deduplication validation
  - Git repo detection tests

benchmark.sh:
  - Performance benchmarking
  - Load testing
  - Response time analysis
  - Memory usage profiling

Usage:
  # Run all tests
  make test

  # Individual test scripts
  ./tests/test-docker.sh      # Build testing
  ./tests/test-incus.sh       # Container testing
  ./tests/test-api.sh         # API testing
  ./tests/benchmark.sh        # Performance testing
```

---

## 🛠️ Development

### Development Workflow

```yaml
Workflow:
  - Docker is used for local builds
  - Incus is used for testing and debugging
  - Iterative process: build → test → fix → repeat
```

### Development Mode

```yaml
Enable:
  --dev flag
  OR DEV=true environment variable
  OR binary named gitignore-dev

Features:
  - Hot reload templates (no restart)
  - Detailed logging (SQL queries, stack traces)
  - Debug endpoints enabled
  - CORS allow all origins
  - Fast session timeout (5 min)

Debug Endpoints:
  GET /debug/routes          - List all routes
  GET /debug/config          - Show configuration
  GET /debug/db              - Database stats
  GET /debug/templates       - Template index stats
  POST /debug/reset          - Reset to fresh state
```

### Local Development

```bash
# Install dependencies
make deps

# Run with hot reload
make run-dev

# Or manually
go run . --dev --port 8080

# Server starts on http://localhost:8080
# Admin credentials displayed in console
```

---

## ✅ Testing

### Test Structure

```
tests/
├── unit/                      # Unit tests
│   ├── templates_test.go      # Template service tests
│   └── database_test.go       # Database tests
├── integration/               # Integration tests
│   ├── api_test.go            # API endpoint tests
│   └── admin_test.go          # Admin auth tests
├── e2e/                       # End-to-end tests
│   └── scenarios_test.go      # E2E test scenarios
├── test-docker.sh             # Docker build testing
├── test-incus.sh              # Incus container testing
├── test-api.sh                # API endpoint testing
├── test-cli.sh                # CLI script testing
└── benchmark.sh               # Performance benchmarking
```

### Running Tests

```bash
# All tests
make test

# Or manually
go test -v -race ./...

# With coverage
go test -v -coverprofile=coverage.out ./...
go tool cover -html=coverage.out

# Benchmarks
go test -v -bench=. -benchmem ./...
```

### Test Coverage Requirements

```yaml
Minimum Coverage: 80%

Critical Paths (100% coverage):
  - Admin authentication
  - Database initialization
  - Settings CRUD
  - Template search/indexing
  - Template combination
```

---

## 🔒 Security

### Best Practices

```yaml
Credentials:
  - Change default admin password immediately
  - Rotate API tokens periodically
  - Use HTTPS in production
  - Restrict admin routes to internal network

File Permissions:
  admin_credentials: 0600 (owner read/write only)
  Database: 0644 (owner write, all read)
  Logs: 0644

Network:
  - Bind to 0.0.0.0 only if needed
  - Use 127.0.0.1 for local-only access
  - Configure firewall rules
  - Use reverse proxy (nginx/Caddy) for HTTPS

Database:
  - Passwords hashed with SHA-256
  - Tokens hashed with SHA-256
  - SQL injection protection (prepared statements)
  - Input validation on all endpoints
```

---

## 📝 License

MIT License - See LICENSE file

### Embedded Data Licenses

```yaml
.gitignore templates:
  Source: Various (GitHub, community contributions)
  License: Public Domain / MIT (varies by template)
```

---

**GitIgnore API Server v1.0** - A comprehensive .gitignore template API with admin-only authentication. Built for simplicity, performance, and ease of deployment.
