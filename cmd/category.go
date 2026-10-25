package cmd

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"
)

var categoryCmd = groupCommand("category", "Inspect and create categories")

var categoryListCmd = &cobra.Command{
	Use:   "list",
	Short: "List the category tree",
	RunE: func(cmd *cobra.Command, args []string) error {
		client, err := newAPIClient()
		if err != nil {
			return err
		}

		rawType, _ := cmd.Flags().GetString("type")
		raw, err := client.ListCategories(cmd.Context(), rawType)
		if err != nil {
			return err
		}
		return emitJSON(raw)
	},
}

var categoryAddCmd = &cobra.Command{
	Use:   "add",
	Short: "Create a second-level category under an existing top-level one",
	RunE: func(cmd *cobra.Command, args []string) error {
		client, err := newAPIClient()
		if err != nil {
			return err
		}

		name, _ := cmd.Flags().GetString("name")
		parent, _ := cmd.Flags().GetString("parent")
		rawType, _ := cmd.Flags().GetString("type")

		if strings.TrimSpace(name) == "" {
			return fmt.Errorf("--name is required")
		}
		if strings.TrimSpace(parent) == "" {
			return fmt.Errorf("--parent is required: a top-level category name, list them with `sumi category list`")
		}
		raw, err := client.AddCategory(cmd.Context(), name, parent, rawType)
		if err != nil {
			return err
		}
		return emitJSON(raw)
	},
}

func init() {
	categoryListCmd.Flags().String("type", "expense", "expense or income")

	categoryAddCmd.Flags().String("name", "", "New category name (required)")
	categoryAddCmd.Flags().String("parent", "", "Existing top-level category name (required)")
	categoryAddCmd.Flags().String("type", "expense", "expense or income")

	categoryCmd.AddCommand(categoryListCmd, categoryAddCmd)
	rootView.AddCommand(categoryCmd)
}
