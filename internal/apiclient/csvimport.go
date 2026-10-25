package apiclient

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"sumi/internal/csvimport"
)

// ImportOptions are the knobs of `sumi bill import` and the import_csv tool.
type ImportOptions struct {
	File            string
	Mapping         map[string]string
	CategoryMap     map[string]string
	DefaultCategory string
	DefaultType     string
	SkipRows        int
	Encoding        string
	Delimiter       string
	SkipInvalid     bool
	KeepZeroAmount  bool
	DryRun          bool
}

// ImportCSV converts a local file and, unless it is a dry run, posts it as one
// batch. A dry run needs no credential, so connect is only called when writing.
//
// The file is read here, not by the caller, so an agent only ever works out the
// mapping from the header and never transcribes an amount.
func ImportCSV(ctx context.Context, opts ImportOptions, connect func() (*Client, error)) (map[string]any, error) {
	file := strings.TrimSpace(opts.File)
	if file == "" {
		return nil, fmt.Errorf("file is required")
	}

	raw, err := os.ReadFile(file)
	if err != nil {
		return nil, fmt.Errorf("cannot read %s: %w", file, err)
	}

	for field := range opts.Mapping {
		switch field {
		case csvimport.FieldDate, csvimport.FieldAmount, csvimport.FieldCategory,
			csvimport.FieldType, csvimport.FieldNote, csvimport.FieldCurrency:
		default:
			return nil, fmt.Errorf("map has unknown field %q: use date, amount, category, type, note or currency", field)
		}
	}

	var comma rune
	if trimmed := strings.TrimSpace(opts.Delimiter); trimmed != "" {
		runes := []rune(trimmed)
		if len(runes) != 1 {
			return nil, fmt.Errorf("delimiter must be a single character, got %q", opts.Delimiter)
		}
		comma = runes[0]
	}
	if opts.DefaultType != "" {
		if _, err := ParseBillType(opts.DefaultType); err != nil {
			return nil, fmt.Errorf("default type: %w", err)
		}
	}

	result, err := csvimport.Parse(raw, csvimport.Options{
		Mapping:         opts.Mapping,
		CategoryMap:     opts.CategoryMap,
		DefaultCategory: strings.TrimSpace(opts.DefaultCategory),
		DefaultType:     strings.TrimSpace(opts.DefaultType),
		SkipRows:        opts.SkipRows,
		Delimiter:       comma,
		Encoding:        opts.Encoding,
		SkipInvalid:     opts.SkipInvalid,
		KeepZeroAmount:  opts.KeepZeroAmount,
	})
	if err != nil {
		return nil, err
	}
	if len(result.Records) == 0 {
		return nil, fmt.Errorf("no importable rows found (%d skipped, %d invalid)", result.Skipped, len(result.Errors))
	}

	payloads := make([]billPayload, 0, len(result.Records))
	for _, record := range result.Records {
		billType, err := ParseBillType(record.Type)
		if err != nil {
			return nil, fmt.Errorf("row %d: %w", record.Row, err)
		}
		payloads = append(payloads, billPayload{
			Type:         billType,
			Amount:       record.Amount,
			Currency:     record.Currency,
			CategoryName: record.Category,
			Description:  record.Note,
			OccurredAt:   record.Date,
		})
	}

	skipped := make([]map[string]any, 0, len(result.Errors))
	for _, rowErr := range result.Errors {
		skipped = append(skipped, map[string]any{"row": rowErr.Row, "reason": rowErr.Reason})
	}

	if opts.DryRun {
		// Show enough to confirm the mapping is right without dumping the file.
		preview := result.Records
		if len(preview) > 5 {
			preview = preview[:5]
		}
		return map[string]any{
			"dry_run":       true,
			"file":          file,
			"header":        result.Header,
			"would_import":  len(payloads),
			"skipped_rows":  result.Skipped,
			"invalid_rows":  skipped,
			"preview":       preview,
			"preview_note":  "first 5 converted rows; re-run without dry run to import",
			"batch_maximum": maxImportRows,
		}, nil
	}

	if len(payloads) > maxImportRows {
		return nil, fmt.Errorf("%d rows exceeds the %d-row limit of one atomic batch; split the file",
			len(payloads), maxImportRows)
	}

	client, err := connect()
	if err != nil {
		return nil, err
	}
	rawResponse, err := client.Do(ctx, "POST", "/api/transactions/batch", nil, map[string]any{"items": payloads})
	if err != nil {
		return nil, err
	}

	var created []billRecord
	if err := json.Unmarshal(rawResponse, &created); err != nil {
		return nil, fmt.Errorf("unexpected response from the server: %w", err)
	}
	return map[string]any{
		"imported":     len(created),
		"skipped_rows": result.Skipped,
		"invalid_rows": skipped,
		"first_id":     firstBillID(created),
		"last_id":      lastBillID(created),
	}, nil
}

// maxImportRows mirrors domain.MaxBatchSize. It is duplicated rather than imported
// so the API client does not link the server's domain package; the server
// enforces the real limit either way.
const maxImportRows = 1000

func firstBillID(bills []billRecord) int64 {
	if len(bills) == 0 {
		return 0
	}
	return bills[0].ID
}

func lastBillID(bills []billRecord) int64 {
	if len(bills) == 0 {
		return 0
	}
	return bills[len(bills)-1].ID
}
