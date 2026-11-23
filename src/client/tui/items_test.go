package tui

import (
	"testing"

	"github.com/apimgr/gitignore/src/common/i18n"
)

func TestListItemsExposeStableLabels(t *testing.T) {
	menu := mainMenuItems(i18n.DefaultLang)[0]
	if menu.Title() != i18n.Translate(i18n.DefaultLang, "cli.client.tui.menu.list") || menu.Description() != i18n.Translate(i18n.DefaultLang, "cli.client.tui.menu.list_description") || menu.FilterValue() != menu.Title() {
		t.Fatalf("menu item labels = %q / %q / %q", menu.Title(), menu.Description(), menu.FilterValue())
	}
	name := nameItem("Go")
	if name.Title() != "Go" || name.Description() != "" || name.FilterValue() != "Go" {
		t.Fatalf("name item labels = %q / %q / %q", name.Title(), name.Description(), name.FilterValue())
	}
}

func TestMainMenuCoversCLICommandsInHelpOrder(t *testing.T) {
	want := []string{"list", "search", "categories", "category", "get", "combine", "stats"}
	items := mainMenuItems(i18n.DefaultLang)
	if len(items) < len(want) {
		t.Fatalf("main menu has %d items; want at least %d", len(items), len(want))
	}
	for index, id := range want {
		if items[index].id != id {
			t.Errorf("main menu item %d = %q, want %q", index, items[index].id, id)
		}
	}
}
