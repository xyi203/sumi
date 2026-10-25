package cmd

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"
)

var statsCmd = groupCommand("stats", "Read spending summaries")

func statsCommand(report, short string) *cobra.Command {
	command := &cobra.Command{
		Use:   report,
		Short: short,
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := newAPIClient()
			if err != nil {
				return err
			}
			month, _ := cmd.Flags().GetString("month")
			rawType := ""
			if cmd.Flags().Lookup("type") != nil {
				rawType, _ = cmd.Flags().GetString("type")
				if strings.TrimSpace(rawType) == "" {
					return fmt.Errorf("invalid --type %q: use expense or income", rawType)
				}
			}
			raw, err := client.GetStats(cmd.Context(), report, month, rawType)
			if err != nil {
				return err
			}
			return emitJSON(raw)
		},
	}
	command.Flags().String("month", "", "Month as YYYY-MM (default: current month)")
	return command
}

func init() {
	statsCategoryCmd := statsCommand("category", "Per-category totals for one month")
	statsCategoryCmd.Flags().String("type", "expense", "expense or income")

	statsCmd.AddCommand(
		statsCommand("monthly", "Income, expense and net total per currency for one month"),
		statsCommand("daily", "Per-day income and expense for one month"),
		statsCategoryCmd,
	)
	rootView.AddCommand(statsCmd)
}
