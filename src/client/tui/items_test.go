package tui

import "testing"

func TestListItemsExposeStableLabels(t *testing.T) {
	menu := mainMenuItems[0]
	if menu.Title() != "List templates" || menu.Description() != "Browse every available template" || menu.FilterValue() != menu.Title() {
		t.Fatalf("menu item labels = %q / %q / %q", menu.Title(), menu.Description(), menu.FilterValue())
	}
	name := nameItem("Go")
	if name.Title() != "Go" || name.Description() != "" || name.FilterValue() != "Go" {
		t.Fatalf("name item labels = %q / %q / %q", name.Title(), name.Description(), name.FilterValue())
	}
}

func TestMainMenuCoversCLICommandsInHelpOrder(t *testing.T) {
	want := []string{"list", "search", "categories", "category", "get", "combine", "stats"}
	if len(mainMenuItems) < len(want) {
		t.Fatalf("main menu has %d items; want at least %d", len(mainMenuItems), len(want))
	}
	for index, id := range want {
		if mainMenuItems[index].id != id {
			t.Errorf("main menu item %d = %q, want %q", index, mainMenuItems[index].id, id)
		}
	}
}
