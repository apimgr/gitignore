package cmd

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/apimgr/gitignore/src/client/api"
	"github.com/apimgr/gitignore/src/client/output"
)

// printNames renders a []string per the requested --output format.
func printNames(names []string, format string, p *output.Printer, tableHeader string) int {
	switch format {
	case "json":
		enc, _ := json.MarshalIndent(names, "", "  ")
		fmt.Println(string(enc))
	case "table":
		rows := make([][]string, len(names))
		for i, n := range names {
			rows[i] = []string{n}
		}
		fmt.Print(output.FormatTable([]string{tableHeader}, rows))
	default:
		for _, n := range names {
			fmt.Println(n)
		}
	}
	return output.ExitSuccess
}

// CmdList implements `gitignore-cli list`.
func CmdList(c *api.Client, p *output.Printer, format string) int {
	names, err := c.List()
	if err != nil {
		return handleAPIError(err, p)
	}
	sort.Strings(names)
	return printNames(names, format, p, text("cli.client.table.template"))
}

// CmdSearch implements `gitignore-cli search QUERY`.
func CmdSearch(c *api.Client, p *output.Printer, format, query string) int {
	if strings.TrimSpace(query) == "" {
		p.ErrorText(text("cli.client.command.search_requires_query", "binary", binaryName()))
		return output.ExitUsage
	}
	names, err := c.Search(query)
	if err != nil {
		return handleAPIError(err, p)
	}
	return printNames(names, format, p, text("cli.client.table.template"))
}

// CmdCategories implements `gitignore-cli categories`.
func CmdCategories(c *api.Client, p *output.Printer, format string) int {
	cats, err := c.Categories()
	if err != nil {
		return handleAPIError(err, p)
	}
	sort.Strings(cats)
	return printNames(cats, format, p, text("cli.client.table.category"))
}

// CmdCategory implements `gitignore-cli category NAME`.
func CmdCategory(c *api.Client, p *output.Printer, format, name string) int {
	if strings.TrimSpace(name) == "" {
		p.ErrorText(text("cli.client.command.category_requires_name", "binary", binaryName()))
		return output.ExitUsage
	}
	names, err := c.CategoryTemplates(name)
	if err != nil {
		return handleAPIError(err, p)
	}
	return printNames(names, format, p, text("cli.client.table.template"))
}

// CmdStats implements `gitignore-cli stats`.
func CmdStats(c *api.Client, p *output.Printer, format string) int {
	stats, err := c.Stats()
	if err != nil {
		return handleAPIError(err, p)
	}
	if format == "json" {
		enc, _ := json.MarshalIndent(stats, "", "  ")
		fmt.Println(string(enc))
		return output.ExitSuccess
	}
	keys := make([]string, 0, len(stats))
	for k := range stats {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		fmt.Printf("%s: %s\n", k, api.StatsCount(stats[k], c.Lang))
	}
	return output.ExitSuccess
}

// CmdGetTemplate implements `gitignore-cli get NAME` / `template NAME`.
func CmdGetTemplate(c *api.Client, p *output.Printer, format, name string) int {
	if strings.TrimSpace(name) == "" {
		p.ErrorText(text("cli.client.command.get_requires_name", "binary", binaryName()))
		return output.ExitUsage
	}
	tmpl, err := c.GetTemplate(name)
	if err != nil {
		return handleAPIError(err, p)
	}
	if format == "json" {
		enc, _ := json.MarshalIndent(tmpl, "", "  ")
		fmt.Println(string(enc))
		return output.ExitSuccess
	}
	fmt.Print(tmpl.Content)
	if !strings.HasSuffix(tmpl.Content, "\n") {
		fmt.Println()
	}
	return output.ExitSuccess
}

// CmdCombine implements `gitignore-cli combine NAME...` and the bare-args
// smart-detection path (`gitignore-cli Go Node`) documented in IDEA.md:
// "gitignore-cli Go Node > .gitignore".
func CmdCombine(c *api.Client, p *output.Printer, format string, names []string) int {
	if len(names) == 0 {
		p.ErrorText(text("cli.client.command.combine_requires_names", "binary", binaryName()))
		return output.ExitUsage
	}
	content, err := c.Combine(names)
	if err != nil {
		return handleAPIError(err, p)
	}
	if format == "json" {
		enc, _ := json.MarshalIndent(map[string]interface{}{
			"templates": names,
			"content":   content,
		}, "", "  ")
		fmt.Println(string(enc))
		return output.ExitSuccess
	}
	fmt.Print(content)
	if !strings.HasSuffix(content, "\n") {
		fmt.Println()
	}
	return output.ExitSuccess
}

// handleAPIError maps an API/connection error to a CLI exit code and prints
// a PART-32-style actionable message.
func handleAPIError(err error, p *output.Printer) int {
	var apiErr *api.APIError
	if errors.As(err, &apiErr) {
		switch apiErr.Status {
		case 404:
			p.ErrorText(text("cli.client.error.resource_not_found", "message", apiErr.Message))
			return output.ExitNotFound
		case 401, 403:
			p.ErrorText(text("cli.client.error.authentication_failed", "message", apiErr.Message))
			return output.ExitAuth
		default:
			p.ErrorText(apiErr.Message)
			return output.ExitGeneral
		}
	}
	p.ErrorText(err.Error())
	fmt.Fprintln(os.Stderr, text("cli.client.error.check_network"))
	fmt.Fprintln(os.Stderr, text("cli.client.error.specify_server"))
	return output.ExitConnection
}

func binaryName() string {
	return BinaryName
}
