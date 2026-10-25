package apiclient

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"
	"strings"
)

type billRecord struct {
	ID          int64  `json:"id"`
	Type        int16  `json:"type"`
	Amount      string `json:"amount"`
	Currency    string `json:"currency"`
	CategoryID  int64  `json:"category_id"`
	Description string `json:"description"`
	OccurredAt  string `json:"occurred_at"`
}

// billPayload mirrors the API's transaction body. Currency is omitted when empty
// so the server can fall back to the user's default currency.
type billPayload struct {
	Type         int16  `json:"type"`
	Amount       string `json:"amount"`
	Currency     string `json:"currency,omitempty"`
	CategoryID   int64  `json:"category_id,omitempty"`
	CategoryName string `json:"category_name,omitempty"`
	Description  string `json:"description,omitempty"`
	OccurredAt   string `json:"occurred_at,omitempty"`
}

// BillInput is one transaction as a caller describes it: the category by name and
// the type as a word. `bill add`, `bill add-batch` and the MCP tools all take this
// shape, so one mental model covers every entry point.
type BillInput struct {
	Amount   string `json:"amount" jsonschema:"amount greater than 0, as a string without a currency symbol, e.g. 25.50"`
	Category string `json:"category" jsonschema:"second-level category name, e.g. 吃; first-level names are rejected"`
	Type     string `json:"type,omitempty" jsonschema:"expense (default) or income"`
	Note     string `json:"note,omitempty" jsonschema:"free-text description"`
	Date     string `json:"date,omitempty" jsonschema:"YYYY-MM-DD; omit for today in the user's own timezone"`
	Currency string `json:"currency,omitempty" jsonschema:"3-letter code; omit for the user's default currency"`
}

func (in BillInput) payload() (billPayload, error) {
	if strings.TrimSpace(in.Amount) == "" {
		return billPayload{}, fmt.Errorf("amount is required")
	}
	if strings.TrimSpace(in.Category) == "" {
		return billPayload{}, fmt.Errorf("category is required (a second-level category name, e.g. 吃)")
	}
	billType, err := ParseBillType(orExpense(in.Type))
	if err != nil {
		return billPayload{}, err
	}
	return billPayload{
		Type:         billType,
		Amount:       strings.TrimSpace(in.Amount),
		Currency:     strings.TrimSpace(in.Currency),
		CategoryName: strings.TrimSpace(in.Category),
		Description:  in.Note,
		OccurredAt:   strings.TrimSpace(in.Date),
	}, nil
}

func (c *Client) AddBill(ctx context.Context, in BillInput) ([]byte, error) {
	payload, err := in.payload()
	if err != nil {
		return nil, err
	}
	return c.Do(ctx, "POST", "/api/transactions/", nil, payload)
}

// AddBills inserts every item in one server-side transaction, so a rejected item
// leaves nothing half applied.
func (c *Client) AddBills(ctx context.Context, items []BillInput) ([]byte, error) {
	if len(items) == 0 {
		return nil, fmt.Errorf("items must contain at least one transaction")
	}
	payloads := make([]billPayload, 0, len(items))
	for i, item := range items {
		payload, err := item.payload()
		if err != nil {
			return nil, fmt.Errorf("items[%d]: %w", i, err)
		}
		payloads = append(payloads, payload)
	}
	return c.Do(ctx, "POST", "/api/transactions/batch", nil, map[string]any{"items": payloads})
}

type BillFilter struct {
	Type     string
	Category string
	Keyword  string
	Currency string
	From     string
	To       string
	Limit    int
	Offset   int
}

func (c *Client) ListBills(ctx context.Context, filter BillFilter) ([]byte, error) {
	query := url.Values{}

	var billType int16
	if strings.TrimSpace(filter.Type) != "" {
		var err error
		billType, err = ParseBillType(filter.Type)
		if err != nil {
			return nil, err
		}
		query.Set("type", strconv.Itoa(int(billType)))
	}
	if category := strings.TrimSpace(filter.Category); category != "" {
		categoryID, err := c.resolveCategoryIDByName(ctx, category, billType)
		if err != nil {
			return nil, err
		}
		query.Set("category_id", strconv.FormatInt(categoryID, 10))
	}
	setIfPresent(query, "keyword", filter.Keyword)
	setIfPresent(query, "currency", filter.Currency)
	setIfPresent(query, "start_time", filter.From)
	setIfPresent(query, "end_time", filter.To)
	query.Set("limit", strconv.Itoa(filter.Limit))
	query.Set("offset", strconv.Itoa(filter.Offset))

	return c.Do(ctx, "GET", "/api/transactions/", query, nil)
}

func billPath(id int64) string {
	return "/api/transactions/" + strconv.FormatInt(id, 10)
}

func ParseBillID(raw string) (int64, error) {
	id, err := strconv.ParseInt(strings.TrimSpace(raw), 10, 64)
	if err != nil || id <= 0 {
		return 0, fmt.Errorf("invalid transaction id %q", raw)
	}
	return id, nil
}

func (c *Client) GetBill(ctx context.Context, id int64) ([]byte, error) {
	return c.Do(ctx, "GET", billPath(id), nil, nil)
}

// BillPatch holds the fields to change; nil leaves a field as it is.
type BillPatch struct {
	Amount   *string
	Category *string
	Type     *string
	Note     *string
	Date     *string
	Currency *string
}

// UpdateBill reads the current row first and resends every field, because the
// API's PUT replaces the whole transaction: sending only the changed field would
// blank out description, currency and date.
func (c *Client) UpdateBill(ctx context.Context, id int64, patch BillPatch) ([]byte, error) {
	existingRaw, err := c.GetBill(ctx, id)
	if err != nil {
		return nil, err
	}
	var existing billRecord
	if err := json.Unmarshal(existingRaw, &existing); err != nil {
		return nil, fmt.Errorf("cannot parse existing transaction: %w", err)
	}

	payload := billPayload{
		Type:        existing.Type,
		Amount:      existing.Amount,
		Currency:    existing.Currency,
		CategoryID:  existing.CategoryID,
		Description: existing.Description,
		OccurredAt:  existing.OccurredAt,
	}
	if patch.Type != nil {
		billType, err := ParseBillType(*patch.Type)
		if err != nil {
			return nil, err
		}
		payload.Type = billType
	}
	if patch.Amount != nil {
		payload.Amount = strings.TrimSpace(*patch.Amount)
	}
	if patch.Currency != nil {
		payload.Currency = strings.TrimSpace(*patch.Currency)
	}
	if patch.Category != nil {
		payload.CategoryID = 0
		payload.CategoryName = strings.TrimSpace(*patch.Category)
	}
	if patch.Note != nil {
		payload.Description = *patch.Note
	}
	if patch.Date != nil {
		payload.OccurredAt = strings.TrimSpace(*patch.Date)
	}

	return c.Do(ctx, "PUT", billPath(id), nil, payload)
}

func (c *Client) DeleteBill(ctx context.Context, id int64) (map[string]any, error) {
	if _, err := c.Do(ctx, "DELETE", billPath(id), nil, nil); err != nil {
		return nil, err
	}
	return map[string]any{"deleted": true, "id": id}, nil
}
