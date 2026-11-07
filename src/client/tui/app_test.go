package tui

import (
	"errors"
	"strings"
	"testing"

	"github.com/apimgr/gitignore/src/client/api"
	clientconfig "github.com/apimgr/gitignore/src/client/config"
	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
)

func applyUpdate(model Model, msg tea.Msg) Model {
	updated, _ := model.Update(msg)
	return updated.(Model)
}

func TestModelScreensAndMessages(t *testing.T) {
	setup := New(nil, nil, "")
	if setup.Init() != nil || !strings.Contains(setup.View(), "setup") {
		t.Fatal("setup model did not initialize")
	}
	updated, _ := setup.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	setup = updated.(Model)
	if setup.width != 100 || setup.height != 30 {
		t.Fatalf("window size = %dx%d", setup.width, setup.height)
	}
	updated, _ = setup.Update(tea.KeyMsg{Type: tea.KeyEnter})
	setup = updated.(Model)
	if !strings.Contains(setup.View(), "valid http") {
		t.Fatal("invalid setup URL was accepted")
	}
	updated, command := setup.Update(tea.KeyMsg{Type: tea.KeyEsc})
	setup = updated.(Model)
	if !setup.quitting || command == nil || setup.View() != "" {
		t.Fatal("setup escape did not quit")
	}

	cfg := clientconfig.Default()
	cfg.Server.Primary = "https://example.test"
	model := New(api.New(cfg.Server.Primary), cfg, "config.yml")
	model = applyUpdate(model, tea.WindowSizeMsg{Width: 100, Height: 30})
	if !strings.Contains(model.View(), "gitignore-cli") {
		t.Fatal("menu view is empty")
	}
	search, _ := model.dispatchMenu("search")
	searchModel := search.(Model)
	if searchModel.screen != screenSearchInput {
		t.Fatal("search menu did not open search input")
	}
	settings, _ := model.dispatchMenu("settings")
	if settings.(Model).screen != screenSetup {
		t.Fatal("settings menu did not open setup")
	}
	if namesTitle(nameModeBrowse) != "Templates (enter to view)" || !strings.Contains(namesTitle(nameModeCategoryDrill), "Category") || !strings.Contains(namesTitle(nameModeCombinePick), "Combine") {
		t.Fatal("name list titles mismatch")
	}

	model = applyUpdate(model, namesLoadedMsg{names: []string{"z", "a"}, mode: nameModeCombinePick})
	if model.screen != screenNames || model.nameMode != nameModeCombinePick {
		t.Fatal("names event did not open names screen")
	}
	if model.names.Items()[0].(nameItem) != "a" {
		t.Fatal("names were not sorted")
	}
	model = applyUpdate(model, templateLoadedMsg{err: errors.New("missing")})
	if model.screen != screenNames || model.errMsg != "missing" {
		t.Fatal("template error state mismatch")
	}
	model = applyUpdate(model, templateLoadedMsg{tmpl: &api.Template{Name: "sample", Content: "body"}})
	if model.screen != screenView || !strings.Contains(model.View(), "sample") {
		t.Fatal("template view did not open")
	}
	model = applyUpdate(model, combineLoadedMsg{content: "combined", names: []string{"a", "b"}})
	if model.screen != screenView || model.viewName != "a + b" {
		t.Fatal("combined view state mismatch")
	}
	model = applyUpdate(model, statsLoadedMsg{stats: map[string]interface{}{"templates": 2, "categories": 1}})
	if model.screen != screenStats || !strings.Contains(model.View(), "templates: 2") {
		t.Fatal("stats view mismatch")
	}
	model = applyUpdate(model, statsLoadedMsg{err: errors.New("stats unavailable")})
	if model.screen != screenMenu || model.errMsg != "stats unavailable" {
		t.Fatal("stats error state mismatch")
	}
	model = applyUpdate(model, configSavedMsg{err: errors.New("write failed")})
	if model.errMsg != "write failed" {
		t.Fatal("config save error missing")
	}
	model = applyUpdate(model, configSavedMsg{})
	if model.screen != screenMenu || model.message != "Server saved." {
		t.Fatal("config save success state mismatch")
	}
}

func TestTUIKeyHandlingAndHelpers(t *testing.T) {
	cfg := clientconfig.Default()
	cfg.Server.Primary = "https://example.test"
	model := New(api.New(cfg.Server.Primary), cfg, "")
	model.names.SetItems([]list.Item{nameItem("sample")})
	model.names.Select(0)
	if model.names.SelectedItem() == nil {
		t.Fatal("name list has no selected item")
	}
	model.screen = screenNames
	model.nameMode = nameModeCombinePick
	updated, _ := model.updateNames(tea.KeyMsg{Type: tea.KeySpace})
	model = updated.(Model)
	if !model.selected["sample"] || len(selectedNames(model.selected)) != 1 {
		t.Fatal("combine selection was not recorded")
	}
	updated, command := model.updateNames(tea.KeyMsg{Type: tea.KeyEnter})
	if command == nil || updated.(Model).screen != screenNames {
		t.Fatal("combine action did not create command")
	}
	updated, _ = model.updateView(tea.KeyMsg{Type: tea.KeyEsc})
	if updated.(Model).screen != screenMenu {
		t.Fatal("escape did not return to menu")
	}
	updated, command = model.handleKey(tea.KeyMsg{Type: tea.KeyCtrlC})
	if !updated.(Model).quitting || command == nil {
		t.Fatal("ctrl+c did not quit")
	}
	if len(selectedNames(map[string]bool{"one": true, "two": false})) != 1 {
		t.Fatal("selectedNames included disabled entries")
	}
}
