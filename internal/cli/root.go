package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/spf13/cobra"

	aiusage "github.com/gustmrg/ai-usage"
	"github.com/gustmrg/ai-usage/internal/app"
	"github.com/gustmrg/ai-usage/internal/model"
	"github.com/gustmrg/ai-usage/internal/provider"
	"github.com/gustmrg/ai-usage/internal/render"
	"github.com/gustmrg/ai-usage/internal/tui"
)

func New(service *app.Service, stdout, stderr io.Writer) *cobra.Command {
	root := &cobra.Command{
		Use:           "ai-usage",
		Short:         "Track AI subscription usage from the terminal",
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runTUI(service, stdout)
		},
	}
	root.SetOut(stdout)
	root.SetErr(stderr)
	root.AddCommand(newTUICommand(service, stdout), newStatusCommand(service), newRefreshCommand(service), newDoctorCommand(service), newVersionCommand())
	return root
}

func newTUICommand(service *app.Service, stdout io.Writer) *cobra.Command {
	return &cobra.Command{Use: "tui", Short: "Open the interactive usage dashboard", RunE: func(cmd *cobra.Command, args []string) error {
		return runTUI(service, stdout)
	}}
}

func runTUI(service *app.Service, stdout io.Writer) error {
	program := tea.NewProgram(tui.New(service), tea.WithOutput(stdout))
	_, err := program.Run()
	return err
}

func newStatusCommand(service *app.Service) *cobra.Command {
	var providerID string
	var jsonOutput bool
	command := &cobra.Command{Use: "status", Short: "Print current provider usage", RunE: func(cmd *cobra.Command, args []string) error {
		return printStatus(cmd.Context(), service, cmd.OutOrStdout(), providerID, jsonOutput, false)
	}}
	command.Flags().StringVarP(&providerID, "provider", "p", "", "provider to query: codex, kimi or opencodego")
	command.Flags().BoolVar(&jsonOutput, "json", false, "print versioned JSON")
	return command
}

func newRefreshCommand(service *app.Service) *cobra.Command {
	var providerID string
	var jsonOutput bool
	command := &cobra.Command{Use: "refresh", Short: "Refresh provider usage now", RunE: func(cmd *cobra.Command, args []string) error {
		return printStatus(cmd.Context(), service, cmd.OutOrStdout(), providerID, jsonOutput, true)
	}}
	command.Flags().StringVarP(&providerID, "provider", "p", "", "provider to refresh: codex, kimi or opencodego")
	command.Flags().BoolVar(&jsonOutput, "json", false, "print versioned JSON")
	return command
}

func printStatus(ctx context.Context, service *app.Service, output io.Writer, providerID string, jsonOutput, force bool) error {
	ctx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	results := map[string]app.Result{}
	if providerID != "" {
		if service.Provider(providerID) == nil {
			return fmt.Errorf("unknown provider %q (want codex, kimi or opencodego)", providerID)
		}
		results[providerID] = service.Fetch(ctx, providerID, force)
	} else {
		results = service.FetchAll(ctx, force)
	}
	if len(results) == 0 {
		return fmt.Errorf("no providers configured; run `codex login`, set KIMI_API_KEY, or set OPENCODE_AUTH_COOKIE")
	}

	report := model.Report{SchemaVersion: model.SchemaVersion, GeneratedAt: time.Now().UTC(), Providers: []model.Snapshot{}, Errors: []model.ProviderError{}}
	ids := make([]string, 0, len(results))
	for id := range results {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		result := results[id]
		if !result.Snapshot.CollectedAt.IsZero() {
			report.Providers = append(report.Providers, result.Snapshot)
		}
		if result.Err != nil {
			report.Errors = append(report.Errors, model.ProviderError{Provider: id, Kind: string(provider.Kind(result.Err)), Message: result.Err.Error()})
		}
	}
	if jsonOutput {
		encoder := json.NewEncoder(output)
		encoder.SetIndent("", "  ")
		if err := encoder.Encode(report); err != nil {
			return err
		}
	} else {
		for index, snapshot := range report.Providers {
			if index > 0 {
				fmt.Fprintln(output)
			}
			fmt.Fprintln(output, render.Snapshot(snapshot, 72, time.Now()))
		}
		for _, item := range report.Errors {
			fmt.Fprintf(output, "\n%s: %s\n", item.Provider, item.Message)
		}
	}
	if len(report.Errors) > 0 && len(report.Providers) == 0 {
		return fmt.Errorf("provider request failed")
	}
	return nil
}

func newDoctorCommand(service *app.Service) *cobra.Command {
	return &cobra.Command{Use: "doctor", Short: "Check provider credentials and local paths", RunE: func(cmd *cobra.Command, args []string) error {
		fmt.Fprintln(cmd.OutOrStdout(), "Provider diagnostics")
		fmt.Fprintln(cmd.OutOrStdout())
		available := 0
		for _, p := range service.Providers() {
			detection := p.Detect()
			status := "missing"
			if detection.Available {
				status = "ready"
				available++
			}
			fmt.Fprintf(cmd.OutOrStdout(), "%-8s %-7s %s\n", p.DisplayName(), status, detection.Detail)
		}
		if available == 0 {
			return fmt.Errorf("no providers configured")
		}
		return nil
	}}
}

func newVersionCommand() *cobra.Command {
	return &cobra.Command{Use: "version", Short: "Print the ai-usage version", Run: func(cmd *cobra.Command, args []string) {
		fmt.Fprintf(cmd.OutOrStdout(), "ai-usage %s\n", aiusage.Version())
	}}
}
