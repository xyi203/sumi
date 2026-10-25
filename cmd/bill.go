package cmd

import (
	"encoding/json"
	"fmt"
	"strings"

	"sumi/internal/apiclient"

	"github.com/spf13/cobra"
)

var billCmd = groupCommand("bill", "Query and record transactions")

var billAddCmd = &cobra.Command{
	Use:   "add",
	Short: "Record one transaction",
	RunE: func(cmd *cobra.Command, args []string) error {
		client, err := newAPIClient()
		if err != nil {
			return err
		}

		flags := cmd.Flags()
		var in apiclient.BillInput
		in.Amount, _ = flags.GetString("amount")
		in.Category, _ = flags.GetString("category")
		in.Type, _ = flags.GetString("type")
		in.Date, _ = flags.GetString("date")
		in.Note, _ = flags.GetString("note")
		in.Currency, _ = flags.GetString("currency")

		if strings.TrimSpace(in.Amount) == "" {
			return fmt.Errorf("--amount is required")
		}
		if strings.TrimSpace(in.Category) == "" {
			return fmt.Errorf("--category is required (a second-level category name, e.g. 吃)")
		}
		if strings.TrimSpace(in.Type) == "" {
			// An explicitly empty --type is a mistake, not a request for the default.
			return fmt.Errorf("invalid --type %q: use expense or income", in.Type)
		}

		raw, err := client.AddBill(cmd.Context(), in)
		if err != nil {
			return err
		}
		return emitJSON(raw)
	},
}

var billAddBatchCmd = &cobra.Command{
	Use:   "add-batch",
	Short: "Record several transactions atomically (all or nothing)",
	RunE: func(cmd *cobra.Command, args []string) error {
		client, err := newAPIClient()
		if err != nil {
			return err
		}

		rawItems, _ := cmd.Flags().GetString("items")
		if strings.TrimSpace(rawItems) == "" {
			return fmt.Errorf("--items is required: a JSON array of {amount, category, type?, note?, date?, currency?}")
		}

		var items []apiclient.BillInput
		if err := json.Unmarshal([]byte(rawItems), &items); err != nil {
			return fmt.Errorf("--items is not a valid JSON array: %w", err)
		}

		raw, err := client.AddBills(cmd.Context(), items)
		if err != nil {
			return err
		}
		return emitJSON(raw)
	},
}

var billListCmd = &cobra.Command{
	Use:   "list",
	Short: "List transactions, newest first",
	RunE: func(cmd *cobra.Command, args []string) error {
		client, err := newAPIClient()
		if err != nil {
			return err
		}

		flags := cmd.Flags()
		var filter apiclient.BillFilter
		filter.Type, _ = flags.GetString("type")
		filter.Category, _ = flags.GetString("category")
		filter.Keyword, _ = flags.GetString("keyword")
		filter.Currency, _ = flags.GetString("currency")
		filter.From, _ = flags.GetString("from")
		filter.To, _ = flags.GetString("to")
		filter.Limit, _ = flags.GetInt("limit")
		filter.Offset, _ = flags.GetInt("offset")

		raw, err := client.ListBills(cmd.Context(), filter)
		if err != nil {
			return err
		}
		return emitJSON(raw)
	},
}

var billGetCmd = &cobra.Command{
	Use:   "get <id>",
	Short: "Show one transaction",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		id, err := apiclient.ParseBillID(args[0])
		if err != nil {
			return err
		}
		client, err := newAPIClient()
		if err != nil {
			return err
		}
		raw, err := client.GetBill(cmd.Context(), id)
		if err != nil {
			return err
		}
		return emitJSON(raw)
	},
}

var billUpdateCmd = &cobra.Command{
	Use:   "update <id>",
	Short: "Change fields of one transaction, leaving unspecified fields intact",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		id, err := apiclient.ParseBillID(args[0])
		if err != nil {
			return err
		}
		client, err := newAPIClient()
		if err != nil {
			return err
		}

		flags := cmd.Flags()
		changed := func(name string) *string {
			if !flags.Changed(name) {
				return nil
			}
			value, _ := flags.GetString(name)
			return &value
		}
		raw, err := client.UpdateBill(cmd.Context(), id, apiclient.BillPatch{
			Amount:   changed("amount"),
			Category: changed("category"),
			Type:     changed("type"),
			Note:     changed("note"),
			Date:     changed("date"),
			Currency: changed("currency"),
		})
		if err != nil {
			return err
		}
		return emitJSON(raw)
	},
}

var billDeleteCmd = &cobra.Command{
	Use:   "del <id>",
	Short: "Delete one transaction",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		id, err := apiclient.ParseBillID(args[0])
		if err != nil {
			return err
		}
		client, err := newAPIClient()
		if err != nil {
			return err
		}
		result, err := client.DeleteBill(cmd.Context(), id)
		if err != nil {
			return err
		}
		return emitValue(result)
	},
}

func init() {
	billAddCmd.Flags().String("amount", "", "Amount, e.g. 25.50 (required)")
	billAddCmd.Flags().String("category", "", "Second-level category name, e.g. 吃 (required)")
	billAddCmd.Flags().String("type", "expense", "expense or income")
	billAddCmd.Flags().String("date", "", "Date as YYYY-MM-DD (default: today)")
	billAddCmd.Flags().String("note", "", "Free-text description")
	billAddCmd.Flags().String("currency", "", "3-letter code (default: the user's default currency)")

	billAddBatchCmd.Flags().String("items", "", "JSON array of {amount, category, type?, note?, date?, currency?} (required)")

	billListCmd.Flags().String("type", "", "expense or income (default: both)")
	billListCmd.Flags().String("category", "", "Filter by second-level category name")
	billListCmd.Flags().String("keyword", "", "Case-insensitive substring match on the note")
	billListCmd.Flags().String("currency", "", "Filter by 3-letter currency code")
	billListCmd.Flags().String("from", "", "Only transactions on or after this date (inclusive)")
	billListCmd.Flags().String("to", "", "Only transactions before this date (exclusive)")
	billListCmd.Flags().Int("limit", 20, "Maximum rows to return")
	billListCmd.Flags().Int("offset", 0, "Rows to skip")

	billUpdateCmd.Flags().String("amount", "", "New amount")
	billUpdateCmd.Flags().String("category", "", "New second-level category name")
	billUpdateCmd.Flags().String("type", "", "expense or income")
	billUpdateCmd.Flags().String("date", "", "New date as YYYY-MM-DD")
	billUpdateCmd.Flags().String("note", "", "New description")
	billUpdateCmd.Flags().String("currency", "", "New 3-letter currency code")

	billCmd.AddCommand(billAddCmd, billAddBatchCmd, billListCmd, billGetCmd, billUpdateCmd, billDeleteCmd)
	rootView.AddCommand(billCmd)
}
