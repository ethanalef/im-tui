package model

import "github.com/charmbracelet/bubbles/key"

type KeyMap struct {
	Tab1     key.Binding
	Tab2     key.Binding
	Tab3     key.Binding
	Tab4     key.Binding
	Tab5     key.Binding
	Tab6     key.Binding
	Tab7     key.Binding
	Tab8     key.Binding
	NextTab  key.Binding
	PrevTab  key.Binding
	Up       key.Binding
	Down     key.Binding
	Left     key.Binding
	Right    key.Binding
	Quit     key.Binding
	Help     key.Binding
	Refresh  key.Binding
	Pause    key.Binding
	EnvNext  key.Binding
}

var Keys = KeyMap{
	Tab1:    key.NewBinding(key.WithKeys("1"), key.WithHelp("1", "Overview")),
	Tab2:    key.NewBinding(key.WithKeys("2"), key.WithHelp("2", "Application")),
	Tab3:    key.NewBinding(key.WithKeys("3"), key.WithHelp("3", "Infrastructure")),
	Tab4:    key.NewBinding(key.WithKeys("4"), key.WithHelp("4", "Kubernetes")),
	Tab5:    key.NewBinding(key.WithKeys("5"), key.WithHelp("5", "Locust")),
	Tab6:    key.NewBinding(key.WithKeys("6"), key.WithHelp("6", "Alerts")),
	Tab7:    key.NewBinding(key.WithKeys("7"), key.WithHelp("7", "Logs")),
	Tab8:    key.NewBinding(key.WithKeys("8"), key.WithHelp("8", "System Map")),
	NextTab: key.NewBinding(key.WithKeys("tab"), key.WithHelp("tab", "next tab")),
	PrevTab: key.NewBinding(key.WithKeys("shift+tab"), key.WithHelp("shift+tab", "prev tab")),
	Up:      key.NewBinding(key.WithKeys("up", "k"), key.WithHelp("k/up", "scroll up")),
	Down:    key.NewBinding(key.WithKeys("down", "j"), key.WithHelp("j/down", "scroll down")),
	Left:    key.NewBinding(key.WithKeys("left", "h"), key.WithHelp("h/←", "scroll left")),
	Right:   key.NewBinding(key.WithKeys("right", "l"), key.WithHelp("l/→", "scroll right")),
	Quit:    key.NewBinding(key.WithKeys("q", "ctrl+c"), key.WithHelp("q", "quit")),
	Help:    key.NewBinding(key.WithKeys("?"), key.WithHelp("?", "help")),
	Refresh: key.NewBinding(key.WithKeys("r"), key.WithHelp("r", "refresh")),
	Pause:   key.NewBinding(key.WithKeys("p"), key.WithHelp("p", "pause")),
	EnvNext: key.NewBinding(key.WithKeys("e"), key.WithHelp("e", "switch env")),
}
