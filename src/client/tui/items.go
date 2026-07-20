package tui

// menuItem is a top-level main-menu entry.
type menuItem struct {
	title string
	desc  string
	id    string
}

func (i menuItem) Title() string       { return i.title }
func (i menuItem) Description() string { return i.desc }
func (i menuItem) FilterValue() string { return i.title }

// nameItem wraps a bare template/category name for list.Model.
type nameItem string

func (i nameItem) Title() string       { return string(i) }
func (i nameItem) Description() string { return "" }
func (i nameItem) FilterValue() string { return string(i) }

var mainMenuItems = []menuItem{
	{id: "list", title: "List templates", desc: "Browse every available template"},
	{id: "search", title: "Search templates", desc: "Search by name, tag, or keyword"},
	{id: "categories", title: "Browse categories", desc: "List templates grouped by category"},
	{id: "combine", title: "Combine templates", desc: "Pick multiple templates and merge them"},
	{id: "stats", title: "Server stats", desc: "Show server-reported template statistics"},
	{id: "settings", title: "Settings", desc: "Change the configured server URL"},
	{id: "quit", title: "Quit", desc: "Exit gitignore-cli"},
}
