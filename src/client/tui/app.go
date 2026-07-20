package tui

import (
	"fmt"
	"sort"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"

	"github.com/apimgr/gitignore/src/client/api"
	"github.com/apimgr/gitignore/src/client/config"
)

// screen identifies which part of the TUI is currently active.
type screen int

const (
	screenSetup screen = iota
	screenMenu
	screenNames
	screenSearchInput
	screenView
	screenStats
	screenMessage
)

// nameListMode distinguishes what a screenNames listing is used for, so a
// single list widget can serve list/search/categories/combine.
type nameListMode int

const (
	nameModeBrowse nameListMode = iota
	nameModeCategoryDrill
	nameModeCombinePick
)

type (
	namesLoadedMsg    struct {
		names []string
		mode  nameListMode
		err   error
	}
	templateLoadedMsg struct {
		tmpl *api.Template
		err  error
	}
	statsLoadedMsg struct {
		stats map[string]interface{}
		err   error
	}
	combineLoadedMsg struct {
		content string
		names   []string
		err     error
	}
	configSavedMsg struct{ err error }
)

// Model is the top-level bubbletea model for gitignore-cli's TUI.
type Model struct {
	client  *api.Client
	cfg     *config.Config
	cfgPath string
	styles  Styles

	screen   screen
	prev     screen
	width    int
	height   int
	quitting bool
	errMsg   string
	message  string

	menu  list.Model
	names list.Model

	nameMode nameListMode
	selected map[string]bool

	input textinput.Model

	viewport viewport.Model
	viewName string

	stats map[string]interface{}
}

// New builds the initial TUI model. cfg/cfgPath let the settings screen
// persist a server URL the same way main.go's flag-to-config logic does.
func New(client *api.Client, cfg *config.Config, cfgPath string) Model {
	styles := defaultStyles()

	menuItems := make([]list.Item, len(mainMenuItems))
	for i, m := range mainMenuItems {
		menuItems[i] = m
	}
	menuList := list.New(menuItems, list.NewDefaultDelegate(), 0, 0)
	menuList.Title = "gitignore-cli"
	menuList.SetShowStatusBar(false)

	namesList := list.New(nil, list.NewDefaultDelegate(), 0, 0)
	namesList.SetShowStatusBar(false)

	ti := textinput.New()
	ti.Placeholder = "https://gitignore.example.com"
	ti.CharLimit = 256
	ti.Prompt = "> "

	m := Model{
		client:   client,
		cfg:      cfg,
		cfgPath:  cfgPath,
		styles:   styles,
		menu:     menuList,
		names:    namesList,
		input:    ti,
		selected: map[string]bool{},
		screen:   screenMenu,
	}

	if cfg == nil || cfg.Server.Primary == "" {
		m.screen = screenSetup
		m.input.Placeholder = "https://gitignore.example.com"
		m.input.Focus()
	}

	return m
}

// Init satisfies tea.Model.
func (m Model) Init() tea.Cmd {
	return nil
}

// Update satisfies tea.Model.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		headerHeight := 4
		m.menu.SetSize(msg.Width, msg.Height-headerHeight)
		m.names.SetSize(msg.Width, msg.Height-headerHeight)
		m.viewport.Width = msg.Width
		m.viewport.Height = msg.Height - headerHeight
		return m, nil

	case tea.KeyMsg:
		return m.handleKey(msg)

	case namesLoadedMsg:
		m.errMsg = ""
		if msg.err != nil {
			m.errMsg = msg.err.Error()
			m.screen = screenMenu
			return m, nil
		}
		sort.Strings(msg.names)
		items := make([]list.Item, len(msg.names))
		for i, n := range msg.names {
			items[i] = nameItem(n)
		}
		m.names.SetItems(items)
		m.names.Title = namesTitle(msg.mode)
		m.nameMode = msg.mode
		m.screen = screenNames
		return m, nil

	case templateLoadedMsg:
		m.errMsg = ""
		if msg.err != nil {
			m.errMsg = msg.err.Error()
			m.screen = screenNames
			return m, nil
		}
		m.viewName = msg.tmpl.Name
		m.viewport = viewport.New(m.width, m.height-4)
		m.viewport.SetContent(msg.tmpl.Content)
		m.screen = screenView
		return m, nil

	case statsLoadedMsg:
		m.errMsg = ""
		if msg.err != nil {
			m.errMsg = msg.err.Error()
			m.screen = screenMenu
			return m, nil
		}
		m.stats = msg.stats
		m.screen = screenStats
		return m, nil

	case combineLoadedMsg:
		m.errMsg = ""
		if msg.err != nil {
			m.errMsg = msg.err.Error()
			m.screen = screenNames
			return m, nil
		}
		m.viewName = strings.Join(msg.names, " + ")
		m.viewport = viewport.New(m.width, m.height-4)
		m.viewport.SetContent(msg.content)
		m.screen = screenView
		return m, nil

	case configSavedMsg:
		if msg.err != nil {
			m.errMsg = msg.err.Error()
			return m, nil
		}
		m.message = "Server saved."
		m.screen = screenMenu
		return m, nil
	}

	return m, nil
}

func namesTitle(mode nameListMode) string {
	switch mode {
	case nameModeCategoryDrill:
		return "Category templates (enter to view)"
	case nameModeCombinePick:
		return "Combine — space to select, enter to combine"
	default:
		return "Templates (enter to view)"
	}
}

func (m Model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if msg.String() == "ctrl+c" {
		m.quitting = true
		return m, tea.Quit
	}

	switch m.screen {
	case screenSetup:
		return m.updateSetup(msg)
	case screenMenu:
		return m.updateMenu(msg)
	case screenNames:
		return m.updateNames(msg)
	case screenSearchInput:
		return m.updateSearchInput(msg)
	case screenView:
		return m.updateView(msg)
	case screenStats, screenMessage:
		if msg.String() == "esc" || msg.String() == "q" || msg.String() == "enter" {
			m.screen = screenMenu
			m.errMsg = ""
		}
		return m, nil
	}
	return m, nil
}

func (m Model) updateSetup(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "enter":
		url := strings.TrimSpace(m.input.Value())
		if !config.IsValidServerURL(url) {
			m.errMsg = "enter a valid http(s) URL"
			return m, nil
		}
		m.cfg.Server.Primary = strings.TrimSuffix(url, "/")
		m.client.BaseURL = m.cfg.Server.Primary
		cfg, path := m.cfg, m.cfgPath
		return m, func() tea.Msg { return configSavedMsg{err: config.Save(path, cfg)} }
	case "esc", "ctrl+q":
		m.quitting = true
		return m, tea.Quit
	}
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	return m, cmd
}

func (m Model) updateMenu(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "q":
		m.quitting = true
		return m, tea.Quit
	case "enter":
		item, ok := m.menu.SelectedItem().(menuItem)
		if !ok {
			return m, nil
		}
		return m.dispatchMenu(item.id)
	}
	var cmd tea.Cmd
	m.menu, cmd = m.menu.Update(msg)
	return m, cmd
}

func (m Model) dispatchMenu(id string) (tea.Model, tea.Cmd) {
	client := m.client
	switch id {
	case "list":
		return m, func() tea.Msg {
			names, err := client.List()
			return namesLoadedMsg{names: names, mode: nameModeBrowse, err: err}
		}
	case "categories":
		return m, func() tea.Msg {
			names, err := client.Categories()
			return namesLoadedMsg{names: names, mode: nameModeCategoryDrill, err: err}
		}
	case "combine":
		m.selected = map[string]bool{}
		return m, func() tea.Msg {
			names, err := client.List()
			return namesLoadedMsg{names: names, mode: nameModeCombinePick, err: err}
		}
	case "stats":
		return m, func() tea.Msg {
			stats, err := client.Stats()
			return statsLoadedMsg{stats: stats, err: err}
		}
	case "search":
		m.input = textinput.New()
		m.input.Placeholder = "search term"
		m.input.Prompt = "> "
		m.input.Focus()
		m.screen = screenSearchInput
		return m, nil
	case "settings":
		m.input = textinput.New()
		m.input.Placeholder = "https://gitignore.example.com"
		m.input.SetValue(m.cfg.Server.Primary)
		m.input.Prompt = "> "
		m.input.Focus()
		m.screen = screenSetup
		return m, nil
	case "quit":
		m.quitting = true
		return m, tea.Quit
	}
	return m, nil
}

func (m Model) updateSearchInput(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "enter":
		q := strings.TrimSpace(m.input.Value())
		if q == "" {
			m.screen = screenMenu
			return m, nil
		}
		client := m.client
		return m, func() tea.Msg {
			names, err := client.Search(q)
			return namesLoadedMsg{names: names, mode: nameModeBrowse, err: err}
		}
	case "esc":
		m.screen = screenMenu
		return m, nil
	}
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	return m, cmd
}

func (m Model) updateNames(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc", "q":
		m.screen = screenMenu
		m.errMsg = ""
		return m, nil
	case " ":
		if m.nameMode == nameModeCombinePick {
			if item, ok := m.names.SelectedItem().(nameItem); ok {
				name := string(item)
				m.selected[name] = !m.selected[name]
			}
			return m, nil
		}
	case "enter":
		item, ok := m.names.SelectedItem().(nameItem)
		if !ok {
			return m, nil
		}
		name := string(item)
		client := m.client
		switch m.nameMode {
		case nameModeCategoryDrill:
			return m, func() tea.Msg {
				names, err := client.CategoryTemplates(name)
				return namesLoadedMsg{names: names, mode: nameModeBrowse, err: err}
			}
		case nameModeCombinePick:
			var picked []string
			for n, on := range m.selected {
				if on {
					picked = append(picked, n)
				}
			}
			if len(picked) == 0 {
				picked = []string{name}
			}
			sort.Strings(picked)
			return m, func() tea.Msg {
				content, err := client.Combine(picked)
				return combineLoadedMsg{content: content, names: picked, err: err}
			}
		default:
			return m, func() tea.Msg {
				tmpl, err := client.GetTemplate(name)
				return templateLoadedMsg{tmpl: tmpl, err: err}
			}
		}
	}
	var cmd tea.Cmd
	m.names, cmd = m.names.Update(msg)
	return m, cmd
}

func (m Model) updateView(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if msg.String() == "esc" || msg.String() == "q" {
		m.screen = screenMenu
		return m, nil
	}
	var cmd tea.Cmd
	m.viewport, cmd = m.viewport.Update(msg)
	return m, cmd
}

// View satisfies tea.Model.
func (m Model) View() string {
	if m.quitting {
		return ""
	}

	var body string
	switch m.screen {
	case screenSetup:
		body = m.styles.Title.Render("gitignore-cli setup") + "\n\n" +
			m.styles.Subtitle.Render("No server is configured yet. Enter the server URL:") + "\n\n" +
			m.input.View() + "\n\n" +
			m.styles.Help.Render("enter: save  ·  esc: quit")
	case screenMenu:
		body = m.menu.View()
	case screenNames:
		body = m.names.View()
		if m.nameMode == nameModeCombinePick {
			body += "\n" + m.styles.Help.Render(fmt.Sprintf("selected: %d", len(selectedNames(m.selected))))
		}
	case screenSearchInput:
		body = m.styles.Title.Render("Search templates") + "\n\n" +
			m.input.View() + "\n\n" +
			m.styles.Help.Render("enter: search  ·  esc: back")
	case screenView:
		body = m.styles.Title.Render(m.viewName) + "\n" + m.viewport.View() + "\n" +
			m.styles.Help.Render("esc: back")
	case screenStats:
		body = m.renderStats()
	}

	if m.errMsg != "" {
		body += "\n\n" + m.styles.Error.Render("Error: "+m.errMsg)
	}
	if m.message != "" {
		body += "\n\n" + m.styles.Success.Render(m.message)
		m.message = ""
	}
	return body
}

func (m Model) renderStats() string {
	var b strings.Builder
	b.WriteString(m.styles.Title.Render("Server stats"))
	b.WriteString("\n\n")
	keys := make([]string, 0, len(m.stats))
	for k := range m.stats {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		b.WriteString(fmt.Sprintf("%s: %s\n", k, api.StatsCount(m.stats[k])))
	}
	b.WriteString("\n" + m.styles.Help.Render("esc: back"))
	return b.String()
}

func selectedNames(sel map[string]bool) []string {
	var out []string
	for n, on := range sel {
		if on {
			out = append(out, n)
		}
	}
	return out
}

// Run starts the bubbletea program and returns once the user quits.
func Run(client *api.Client, cfg *config.Config, cfgPath string) error {
	p := tea.NewProgram(New(client, cfg, cfgPath), tea.WithAltScreen())
	_, err := p.Run()
	return err
}
