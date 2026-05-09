package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	tea "github.com/charmbracelet/bubbletea"

	"agentswitcher/internal/app"
	"agentswitcher/internal/store"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	repo, err := store.NewRepository("agentswitcher.db")
	if err != nil {
		exitWithError(err)
	}
	defer repo.Close()

	model, err := app.NewModel(repo)
	if err != nil {
		exitWithError(err)
	}

	program := tea.NewProgram(model, tea.WithAltScreen(), tea.WithContext(ctx), tea.WithMouseCellMotion())
	if _, err := program.Run(); err != nil && !errors.Is(err, context.Canceled) {
		exitWithError(err)
	}
}

func exitWithError(err error) {
	fmt.Fprintln(os.Stderr, err)
	os.Exit(1)
}
