package cmd

import (
	"fmt"
	"strings"

	"sumi/internal/apiclient"

	"github.com/spf13/cobra"
)

// billImportCmd converts a CSV locally and sends it as one atomic batch. Parsing
// happens here rather than server-side because the file, its encoding and its
// column quirks are all local concerns; the server only ever sees clean JSON.
//
// The intended division of labour with an agent: the agent reads only the header
// (e.g. `head -20 file.csv`) and works out --map / --category-map / --skip-rows,
// then this command converts every row exactly and inserts them in one
// transaction. The agent never has to read the data rows, so it cannot mistype an
// amount and the import cannot land half-applied.
var billImportCmd = &cobra.Command{
	Use:   "import",
	Short: "Import transactions from a CSV file in one atomic batch",
	Long: `Import transactions from a CSV file.

All rows are inserted in a single transaction: if any row is rejected, nothing is
imported. Use --dry-run first to see what would be sent.

Columns are located with --map, which accepts either a header name or a 1-based
column number:

  sumi bill import --file bill.csv \
    --map date=交易时间,amount=金额,note=商品说明,type=收/支,category=交易分类 \
    --category-map "餐饮美食=吃,交通出行=行" \
    --skip-rows 3 --dry-run

Without --map, columns named date/amount/category/type/note/currency are used.
GBK files and a UTF-8 BOM are handled automatically; rows with a zero amount are
skipped, as wallet exports use those for non-monetary entries.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		flags := cmd.Flags()
		var opts apiclient.ImportOptions
		var err error
		opts.File, _ = flags.GetString("file")
		if opts.Mapping, err = parseKeyValueFlag(cmd, "map"); err != nil {
			return err
		}
		if opts.CategoryMap, err = parseKeyValueFlag(cmd, "category-map"); err != nil {
			return err
		}
		opts.SkipRows, _ = flags.GetInt("skip-rows")
		opts.Encoding, _ = flags.GetString("encoding")
		opts.Delimiter, _ = flags.GetString("delimiter")
		opts.DefaultCategory, _ = flags.GetString("default-category")
		opts.DefaultType, _ = flags.GetString("default-type")
		opts.SkipInvalid, _ = flags.GetBool("skip-invalid")
		opts.KeepZeroAmount, _ = flags.GetBool("keep-zero-amount")
		opts.DryRun, _ = flags.GetBool("dry-run")

		result, err := apiclient.ImportCSV(cmd.Context(), opts, newAPIClient)
		if err != nil {
			return err
		}
		return emitValue(result)
	},
}

// parseKeyValueFlag reads a repeatable "k=v,k=v" flag. Values may contain "=" so
// only the first one separates key from value.
func parseKeyValueFlag(cmd *cobra.Command, name string) (map[string]string, error) {
	entries, _ := cmd.Flags().GetStringSlice(name)
	if len(entries) == 0 {
		return nil, nil
	}

	parsed := make(map[string]string, len(entries))
	for _, entry := range entries {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}
		key, value, found := strings.Cut(entry, "=")
		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)
		if !found || key == "" || value == "" {
			return nil, fmt.Errorf("--%s expects key=value pairs, got %q", name, entry)
		}
		parsed[key] = value
	}
	return parsed, nil
}

func init() {
	billImportCmd.Flags().String("file", "", "Path to the CSV file (required)")
	billImportCmd.Flags().StringSlice("map", nil,
		"Column mapping, e.g. date=交易时间,amount=金额 (header name or 1-based number)")
	billImportCmd.Flags().StringSlice("category-map", nil,
		"Rename source categories, e.g. 餐饮美食=吃,交通出行=行")
	billImportCmd.Flags().String("default-category", "", "Category for rows that have none")
	billImportCmd.Flags().String("default-type", "expense", "expense or income, when no type column is mapped")
	billImportCmd.Flags().Int("skip-rows", 0, "Lines to drop before the header row")
	billImportCmd.Flags().String("encoding", "", "utf8 or gbk (default: auto-detect)")
	billImportCmd.Flags().String("delimiter", "", "Field delimiter (default: comma)")
	billImportCmd.Flags().Bool("skip-invalid", false, "Skip unconvertible rows instead of aborting the file")
	billImportCmd.Flags().Bool("keep-zero-amount", false, "Keep rows whose amount is zero (dropped by default)")
	billImportCmd.Flags().Bool("dry-run", false, "Show what would be imported without writing anything")

	billCmd.AddCommand(billImportCmd)
}
