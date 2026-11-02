package main

import (
	"errors"
	"fmt"
	"strings"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
)

type hostPrompt struct {
	input   textinput.Model
	host    string
	problem string
	done    bool
}

func newHostPrompt() hostPrompt {
	input := textinput.New()
	input.Placeholder = "alias SSH configurado em ~/.ssh/config"
	input.CharLimit = 128
	input.SetWidth(56)
	input.Focus()
	return hostPrompt{input: input}
}

func (model hostPrompt) Init() tea.Cmd {
	return textinput.Blink
}

func (model hostPrompt) Update(message tea.Msg) (tea.Model, tea.Cmd) {
	key, ok := message.(tea.KeyPressMsg)
	if ok {
		switch key.String() {
		case "ctrl+c", "esc":
			return model, tea.Quit
		case "enter":
			host := strings.TrimSpace(model.input.Value())
			if err := validateHostAlias(host); err != nil {
				model.problem = err.Error()
				return model, nil
			}
			model.host = host
			model.done = true
			return model, tea.Quit
		}
	}
	var command tea.Cmd
	model.input, command = model.input.Update(message)
	return model, command
}

func (model hostPrompt) View() tea.View {
	if model.done {
		return tea.NewView("Host SSH selecionado: " + model.host + "\n")
	}
	view := "Preparo da VPS para o runner self-hosted\n\nAlias SSH (obrigatório):\n" + model.input.View() + "\n\nEnter confirma · Esc cancela"
	if model.problem != "" {
		view += "\n\n" + model.problem
	}
	return tea.NewView(view)
}

type usernamePrompt struct {
	input    textinput.Model
	username string
	problem  string
	done     bool
}

func newUsernamePrompt() usernamePrompt {
	input := textinput.New()
	input.Placeholder = defaultRunnerUsername
	input.CharLimit = 32
	input.SetWidth(56)
	input.Focus()
	return usernamePrompt{input: input}
}

func (model usernamePrompt) Init() tea.Cmd {
	return textinput.Blink
}

func (model usernamePrompt) Update(message tea.Msg) (tea.Model, tea.Cmd) {
	key, ok := message.(tea.KeyPressMsg)
	if ok {
		switch key.String() {
		case "ctrl+c", "esc":
			return model, tea.Quit
		case "enter":
			username := strings.TrimSpace(model.input.Value())
			if username == "" {
				username = defaultRunnerUsername
			}
			if err := validateRunnerUsername(username); err != nil {
				model.problem = err.Error()
				return model, nil
			}
			model.username = username
			model.done = true
			return model, tea.Quit
		}
	}
	var command tea.Cmd
	model.input, command = model.input.Update(message)
	return model, command
}

func (model usernamePrompt) View() tea.View {
	if model.done {
		return tea.NewView("Usuário do runner: " + model.username + "\n")
	}
	view := "Usuário do runner, sem sudo\n\nEnter vazio usa " + defaultRunnerUsername + ":\n" + model.input.View() + "\n\nEnter confirma · Esc cancela"
	if model.problem != "" {
		view += "\n\n" + model.problem
	}
	return tea.NewView(view)
}

type confirmPrompt struct {
	summary  string
	options  []string
	selected int
	done     bool
	accepted bool
}

func newConfirmPrompt(summary string) confirmPrompt {
	return confirmPrompt{
		summary: summary,
		options: []string{"aplicar", "cancelar"},
	}
}

func (model confirmPrompt) Init() tea.Cmd {
	return nil
}

func (model confirmPrompt) Update(message tea.Msg) (tea.Model, tea.Cmd) {
	key, ok := message.(tea.KeyPressMsg)
	if !ok {
		return model, nil
	}
	switch key.String() {
	case "ctrl+c", "esc":
		return model, tea.Quit
	case "up", "k":
		if model.selected > 0 {
			model.selected--
		}
	case "down", "j":
		if model.selected < len(model.options)-1 {
			model.selected++
		}
	case "enter":
		model.done = true
		model.accepted = model.options[model.selected] == "aplicar"
		return model, tea.Quit
	}
	return model, nil
}

func (model confirmPrompt) View() tea.View {
	view := model.summary + "\n"
	for index, option := range model.options {
		marker := "  "
		if index == model.selected {
			marker = "> "
		}
		view += marker + option + "\n"
	}
	return tea.NewView(view + "\n↑/↓ seleciona · Enter confirma · Esc cancela")
}

func promptHost() (string, error) {
	result, err := tea.NewProgram(newHostPrompt()).Run()
	if err != nil {
		return "", fmt.Errorf("ler alias SSH: %w", err)
	}
	selection, ok := result.(hostPrompt)
	if !ok || !selection.done {
		return "", errors.New("seleção do host cancelada")
	}
	return selection.host, nil
}

func promptUsername() (string, error) {
	result, err := tea.NewProgram(newUsernamePrompt()).Run()
	if err != nil {
		return "", fmt.Errorf("ler usuário do runner: %w", err)
	}
	selection, ok := result.(usernamePrompt)
	if !ok || !selection.done {
		return "", errors.New("seleção do usuário cancelada")
	}
	return selection.username, nil
}

func promptConfirm(summary string) (bool, error) {
	result, err := tea.NewProgram(newConfirmPrompt(summary)).Run()
	if err != nil {
		return false, fmt.Errorf("ler confirmação: %w", err)
	}
	selection, ok := result.(confirmPrompt)
	if !ok || !selection.done {
		return false, nil
	}
	return selection.accepted, nil
}
