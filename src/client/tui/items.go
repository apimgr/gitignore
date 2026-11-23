package tui

import "github.com/apimgr/gitignore/src/common/i18n"

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

func mainMenuItems(lang string) []menuItem {
	entries := []struct {
		id    string
		title string
		desc  string
	}{
		{id: "list", title: "cli.client.tui.menu.list", desc: "cli.client.tui.menu.list_description"},
		{id: "search", title: "cli.client.tui.menu.search", desc: "cli.client.tui.menu.search_description"},
		{id: "categories", title: "cli.client.tui.menu.categories", desc: "cli.client.tui.menu.categories_description"},
		{id: "category", title: "cli.client.tui.menu.category", desc: "cli.client.tui.menu.category_description"},
		{id: "get", title: "cli.client.tui.menu.get", desc: "cli.client.tui.menu.get_description"},
		{id: "combine", title: "cli.client.tui.menu.combine", desc: "cli.client.tui.menu.combine_description"},
		{id: "stats", title: "cli.client.tui.menu.stats", desc: "cli.client.tui.menu.stats_description"},
		{id: "settings", title: "cli.client.tui.menu.settings", desc: "cli.client.tui.menu.settings_description"},
		{id: "quit", title: "cli.client.tui.menu.quit", desc: "cli.client.tui.menu.quit_description"},
	}
	items := make([]menuItem, len(entries))
	for index, entry := range entries {
		items[index] = menuItem{
			id:    entry.id,
			title: i18n.Translate(lang, entry.title),
			desc:  i18n.Translate(lang, entry.desc),
		}
	}
	return items
}
