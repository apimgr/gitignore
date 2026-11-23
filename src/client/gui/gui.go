//go:build gui && (linux || darwin || windows)

// Package gui implements the native pure-Go desktop client.
package gui

import (
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/apimgr/gitignore/src/client/api"
	"github.com/apimgr/gitignore/src/client/config"
	"github.com/apimgr/gitignore/src/common/display"
	displayformat "github.com/apimgr/gitignore/src/common/format"
	"github.com/apimgr/gitignore/src/common/i18n"
	"github.com/gogpu/gogpu"
	"github.com/gogpu/ui/app"
	"github.com/gogpu/ui/core/button"
	"github.com/gogpu/ui/core/checkbox"
	"github.com/gogpu/ui/core/dialog"
	"github.com/gogpu/ui/core/listview"
	"github.com/gogpu/ui/core/scrollview"
	"github.com/gogpu/ui/core/textfield"
	"github.com/gogpu/ui/desktop"
	"github.com/gogpu/ui/primitives"
	"github.com/gogpu/ui/state"
	"github.com/gogpu/ui/theme/material3"
	"github.com/gogpu/ui/widget"
)

// IsGUIAvailable reports whether a supported local native display is active.
// Remote SSH and Mosh sessions always use the TUI even when a forwarded
// display variable is present.
func IsGUIAvailable() bool {
	env := display.DetectDisplayEnv()
	return env.HasDisplay && !env.IsSSH && !env.IsMosh
}

func newMaterialTheme() *material3.Theme {
	seed := widget.Hex(0x6750A4)
	if isSystemDark() {
		return material3.NewDark(seed)
	}
	return material3.New(seed)
}

// LaunchGUI opens the native window and provides the same catalog operations
// as the CLI and TUI: browse, search, categories, combine, statistics, and
// server configuration.
func LaunchGUI(client *api.Client, cfg *config.Config, cfgPath string) error {
	if !IsGUIAvailable() {
		return errors.New(i18n.Translate(clientLanguage(client), "cli.client.gui.unavailable"))
	}
	if cfg == nil {
		cfg = config.Default()
	}
	if client == nil {
		client = api.New(cfg.Server.Primary)
	}
	if client.Lang == "" {
		client.Lang = cfg.Lang
	}
	if client.BaseURL == "" {
		client.BaseURL = cfg.Server.Primary
	}

	theme := newMaterialTheme()
	gogpuApp := gogpu.NewApp(gogpu.DefaultConfig().WithTitle(i18n.Translate(client.Lang, "cli.client.gui.window_title")).WithSize(1000, 720))
	uiApp := app.New(
		app.WithWindowProvider(gogpuApp),
		app.WithPlatformProvider(gogpuApp),
		app.WithEventSource(gogpuApp.EventSource()),
		app.WithTheme(theme.AsTheme()),
	)
	ui := &window{
		client:   client,
		cfg:      cfg,
		cfgPath:  cfgPath,
		app:      uiApp,
		theme:    theme,
		selected: map[string]bool{},
	}
	ui.showHome()
	if err := desktop.Run(gogpuApp, uiApp); err != nil {
		return fmt.Errorf("%s", i18n.Translate(client.Lang, "cli.client.gui.launch_failed"))
	}
	return nil
}

func clientLanguage(client *api.Client) string {
	if client != nil {
		return client.Lang
	}
	return i18n.DefaultLang
}

type window struct {
	client   *api.Client
	cfg      *config.Config
	cfgPath  string
	app      *app.App
	theme    *material3.Theme
	selected map[string]bool
}

func (w *window) text(key string, args ...interface{}) string {
	return i18n.TranslateFormat(clientLanguage(w.client), key, args...)
}

func (w *window) showHome() {
	if w.client.BaseURL == "" {
		w.showSettings(true)
		return
	}
	w.setRoot(w.text("cli.client.gui.home_title"), w.text("cli.client.gui.home_description"),
		w.action(w.text("cli.client.gui.browse_templates"), func() { w.loadNames(w.client.List, w.text("cli.client.gui.templates")) }),
		w.action(w.text("cli.client.gui.search_templates"), w.showSearch),
		w.action(w.text("cli.client.gui.browse_categories"), func() { w.loadNames(w.client.Categories, w.text("cli.client.gui.categories")) }),
		w.action(w.text("cli.client.gui.category_templates"), w.showCategory),
		w.action(w.text("cli.client.gui.get_template"), func() { w.loadNames(w.client.List, w.text("cli.client.gui.templates")) }),
		w.action(w.text("cli.client.gui.combine_templates"), w.showCombine),
		w.action(w.text("cli.client.gui.catalog_statistics"), w.showStats),
		w.action(w.text("cli.client.gui.server_settings"), func() { w.showSettings(false) }),
	)
}

func (w *window) showCategory() {
	name := state.NewSignal("")
	field := textfield.New(
		textfield.Placeholder(w.text("cli.client.gui.category_name")),
		textfield.ValueSignal(name),
		textfield.A11yLabel(w.text("cli.client.gui.category_name")),
		textfield.PainterOpt(material3.TextFieldPainter{Theme: w.theme}),
	)
	w.setRoot(w.text("cli.client.gui.category_title"), w.text("cli.client.gui.category_prompt"),
		field,
		w.action(w.text("cli.client.gui.show_templates"), func() {
			category := strings.TrimSpace(name.Get())
			if category == "" {
				w.showError(fmt.Errorf("%s", w.text("cli.client.gui.category_required")))
				return
			}
			w.loadNames(func() ([]string, error) { return w.client.CategoryTemplates(category) }, category)
		}),
		w.action(w.text("cli.client.gui.back"), w.showHome),
	)
}

func (w *window) action(label string, callback func()) widget.Widget {
	return button.New(
		button.TextOpt(label),
		button.OnClick(callback),
		button.PainterOpt(material3.ButtonPainter{Theme: w.theme}),
	)
}

func (w *window) setRoot(title, message string, children ...widget.Widget) {
	items := []widget.Widget{
		primitives.Text(title).FontSize(28).Bold().Color(w.theme.Colors.OnSurface),
		primitives.Text(message).FontSize(15).Color(w.theme.Colors.OnSurfaceVariant),
	}
	items = append(items, children...)
	w.app.SetRoot(primitives.Box(items...).Padding(24).Gap(12).Background(w.theme.Colors.Surface))
}

func (w *window) showError(err error) {
	ctx := w.app.Window().Context()
	previousFocus := ctx.FocusedWidget()
	message := errorMessage(err)
	alert := dialog.New(
		dialog.Title(w.text("cli.client.gui.request_failed")),
		dialog.Content(primitives.Text(message).FontSize(15).Color(w.theme.Colors.OnSurface)),
		dialog.Actions(dialog.Action{
			Label:   w.text("cli.client.gui.ok"),
			Variant: dialog.VariantFilled,
			Default: true,
		}),
		dialog.OnClose(func() {
			if previousFocus != nil {
				ctx.RequestFocus(previousFocus)
			}
		}),
		dialog.PainterOpt(material3.DialogPainter{Theme: w.theme}),
	)
	alert.Show(ctx)
}

func errorMessage(err error) string {
	var apiErr *api.APIError
	if errors.As(err, &apiErr) {
		return apiErr.Message
	}
	return err.Error()
}

func (w *window) loadNames(load func() ([]string, error), title string) {
	names, err := load()
	if err != nil {
		w.showError(err)
		return
	}
	sort.Strings(names)
	w.showNames(title, names)
}

func (w *window) showNames(title string, names []string) {
	items := make([]widget.Widget, 0, 2)
	items = append(items, w.action(w.text("cli.client.gui.back"), w.showHome))
	list := listview.New(
		listview.ItemCount(len(names)),
		listview.FixedItemHeight(42),
		listview.SelectionModeOpt(listview.SelectionSingle),
		listview.PainterOpt(material3.ListViewPainter{Theme: w.theme}),
		listview.BuildItem(func(ctx listview.ItemContext) widget.Widget {
			return primitives.Text(names[ctx.Index]).FontSize(16).Color(w.theme.Colors.OnSurface)
		}),
		listview.OnItemClick(func(index int) {
			if index < 0 || index >= len(names) {
				return
			}
			if title == w.text("cli.client.gui.categories") {
				w.loadNames(func() ([]string, error) { return w.client.CategoryTemplates(names[index]) }, names[index])
				return
			}
			w.showTemplate(names[index])
		}),
	)
	items = append(items, primitives.Box(list).Height(520).Rounded(8).Background(widget.RGBA8(255, 255, 255, 255)))
	w.setRoot(title, w.text("cli.client.gui.select_item_prompt"), items...)
}

func (w *window) showTemplate(name string) {
	tmpl, err := w.client.GetTemplate(name)
	if err != nil {
		w.showError(err)
		return
	}
	content := scrollview.New(primitives.Text(tmpl.Content).FontSize(13).Color(w.theme.Colors.OnSurface))
	w.setRoot(tmpl.Name, fmt.Sprintf("%s · %s", tmpl.FileName, displayformat.Size(int64(tmpl.Size), w.client.Lang)),
		w.action(w.text("cli.client.gui.back"), w.showHome),
		primitives.Box(content).Height(520).Padding(12).Background(w.theme.Colors.SurfaceVariant),
	)
}

func (w *window) showSearch() {
	query := state.NewSignal("")
	field := textfield.New(
		textfield.Placeholder(w.text("cli.client.gui.search_placeholder")),
		textfield.ValueSignal(query),
		textfield.A11yLabel(w.text("cli.client.gui.search_query")),
		textfield.PainterOpt(material3.TextFieldPainter{Theme: w.theme}),
	)
	w.setRoot(w.text("cli.client.gui.search_title"), w.text("cli.client.gui.search_prompt"),
		field,
		w.action(w.text("cli.client.gui.search"), func() {
			w.loadNames(func() ([]string, error) { return w.client.Search(query.Get()) }, w.text("cli.client.gui.search_results"))
		}),
		w.action(w.text("cli.client.gui.back"), w.showHome),
	)
}

func (w *window) showCombine() {
	names, err := w.client.List()
	if err != nil {
		w.showError(err)
		return
	}
	sort.Strings(names)
	w.selected = map[string]bool{}
	items := []widget.Widget{w.action(w.text("cli.client.gui.back"), w.showHome)}
	list := listview.New(
		listview.ItemCount(len(names)),
		listview.FixedItemHeight(42),
		listview.BuildItem(func(ctx listview.ItemContext) widget.Widget {
			name := names[ctx.Index]
			return checkbox.New(
				checkbox.LabelOpt(name),
				checkbox.Checked(w.selected[name]),
				checkbox.OnToggle(func(checked bool) { w.selected[name] = checked }),
			)
		}),
	)
	items = append(items,
		primitives.Box(list).Height(480).Rounded(8).Background(w.theme.Colors.SurfaceVariant),
		w.action(w.text("cli.client.gui.combine_selected"), func() { w.combineSelected() }),
	)
	w.setRoot(w.text("cli.client.gui.combine_title"), w.text("cli.client.gui.combine_prompt"), items...)
}

func (w *window) combineSelected() {
	names := make([]string, 0, len(w.selected))
	for name, selected := range w.selected {
		if selected {
			names = append(names, name)
		}
	}
	if len(names) == 0 {
		w.showError(fmt.Errorf("%s", w.text("cli.client.gui.select_template_required")))
		return
	}
	sort.Strings(names)
	content, err := w.client.Combine(names)
	if err != nil {
		w.showError(err)
		return
	}
	view := scrollview.New(primitives.Text(content).FontSize(12).Color(w.theme.Colors.OnSurface))
	w.setRoot(w.text("cli.client.gui.combined_title"), strings.Join(names, " + "),
		w.action(w.text("cli.client.gui.back"), w.showHome),
		primitives.Box(view).Height(520).Padding(12).Background(w.theme.Colors.SurfaceVariant),
	)
}

func (w *window) showStats() {
	stats, err := w.client.Stats()
	if err != nil {
		w.showError(err)
		return
	}
	keys := make([]string, 0, len(stats))
	for key := range stats {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	rows := make([]widget.Widget, 0, len(keys)+1)
	for _, key := range keys {
		rows = append(rows, primitives.Text(fmt.Sprintf("%s: %s", key, api.StatsCount(stats[key], w.client.Lang))).FontSize(16))
	}
	rows = append(rows, w.action(w.text("cli.client.gui.back"), w.showHome))
	w.setRoot(w.text("cli.client.gui.catalog_statistics"), w.text("cli.client.gui.stats_description"), rows...)
}

func (w *window) showSettings(setup bool) {
	server := state.NewSignal(w.cfg.Server.Primary)
	field := textfield.New(
		textfield.Placeholder(w.text("cli.client.gui.server_url_placeholder")),
		textfield.ValueSignal(server),
		textfield.A11yLabel(w.text("cli.client.gui.server_url")),
		textfield.PainterOpt(material3.TextFieldPainter{Theme: w.theme}),
	)
	message := w.text("cli.client.gui.settings_description")
	if setup {
		message = w.text("cli.client.gui.setup_description")
	}
	children := []widget.Widget{
		field,
		w.action(w.text("cli.client.gui.test_and_save"), func() {
			url := strings.TrimSpace(server.Get())
			if !config.IsValidServerURL(url) {
				w.showError(fmt.Errorf("%s", w.text("cli.client.gui.invalid_server_url")))
				return
			}
			candidate := api.New(strings.TrimSuffix(url, "/"))
			candidate.Lang = w.client.Lang
			if _, err := candidate.List(); err != nil {
				w.showError(err)
				return
			}
			w.cfg.Server.Primary = candidate.BaseURL
			w.client.BaseURL = candidate.BaseURL
			if err := config.Save(w.cfgPath, w.cfg); err != nil {
				w.showError(fmt.Errorf("%s", w.text("cli.client.gui.save_failed")))
				return
			}
			w.showHome()
		}),
	}
	if !setup {
		children = append(children, w.action(w.text("cli.client.gui.back"), w.showHome))
	}
	w.setRoot(w.text("cli.client.gui.server_settings"), message, children...)
}
