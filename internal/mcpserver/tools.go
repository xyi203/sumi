package mcpserver

import (
	"context"
	"strings"
	"time"

	"sumi/internal/domain"
	"sumi/internal/services"
	"sumi/pkg/errorx"

	"github.com/google/uuid"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/shopspring/decimal"
)

const dateLayout = "2006-01-02"

// billInput is one transaction as an agent describes it: the category by name
// and the type as a word.
type billInput struct {
	Amount   string `json:"amount" jsonschema:"amount greater than 0, as a string without a currency symbol, e.g. 25.50"`
	Category string `json:"category" jsonschema:"second-level category name, e.g. 吃; first-level names are rejected"`
	Type     string `json:"type,omitempty" jsonschema:"expense (default) or income"`
	Note     string `json:"note,omitempty" jsonschema:"free-text description"`
	Date     string `json:"date,omitempty" jsonschema:"YYYY-MM-DD; omit for today in the user's own timezone"`
	Currency string `json:"currency,omitempty" jsonschema:"3-letter code; omit for the user's default currency"`
}

type idInput struct {
	ID int64 `json:"id" jsonschema:"transaction id, from list_bills"`
}

type addBillsInput struct {
	Items []billInput `json:"items" jsonschema:"transactions to insert in one transaction, at most 1000"`
}

type listBillsInput struct {
	Type     string `json:"type,omitempty" jsonschema:"expense or income; omit for both"`
	Category string `json:"category,omitempty" jsonschema:"second-level category name"`
	Keyword  string `json:"keyword,omitempty" jsonschema:"case-insensitive substring of the note"`
	Currency string `json:"currency,omitempty" jsonschema:"3-letter currency code"`
	From     string `json:"from,omitempty" jsonschema:"YYYY-MM-DD, inclusive"`
	To       string `json:"to,omitempty" jsonschema:"YYYY-MM-DD, exclusive: all of August is from 2026-08-01 to 2026-09-01"`
	Limit    int    `json:"limit,omitempty" jsonschema:"maximum rows, default 20"`
	Offset   int    `json:"offset,omitempty" jsonschema:"rows to skip"`
}

type updateBillInput struct {
	ID       int64   `json:"id" jsonschema:"transaction id, from list_bills"`
	Amount   *string `json:"amount,omitempty" jsonschema:"new amount"`
	Category *string `json:"category,omitempty" jsonschema:"new second-level category name"`
	Type     *string `json:"type,omitempty" jsonschema:"expense or income"`
	Note     *string `json:"note,omitempty" jsonschema:"new description"`
	Date     *string `json:"date,omitempty" jsonschema:"new date as YYYY-MM-DD"`
	Currency *string `json:"currency,omitempty" jsonschema:"new 3-letter currency code"`
}

type categoryTypeInput struct {
	Type string `json:"type,omitempty" jsonschema:"expense (default) or income"`
}

type addCategoryInput struct {
	Name   string `json:"name" jsonschema:"new second-level category name"`
	Parent string `json:"parent" jsonschema:"existing first-level category name"`
	Type   string `json:"type,omitempty" jsonschema:"expense (default) or income"`
}

type statsInput struct {
	Report string `json:"report" jsonschema:"monthly (totals per currency), daily (per day) or category (per category)"`
	Month  string `json:"month,omitempty" jsonschema:"YYYY-MM; omit for the current month"`
	Type   string `json:"type,omitempty" jsonschema:"expense (default) or income; only for the category report"`
}

// billView answers in the words the tools take, so an agent can feed a listed
// bill back into update_bill without translating 1/2 or timestamps.
type billView struct {
	ID         int64  `json:"id"`
	Type       string `json:"type"`
	Amount     string `json:"amount"`
	Currency   string `json:"currency"`
	CategoryID int64  `json:"category_id"`
	Note       string `json:"note"`
	Date       string `json:"date"`
}

func toBillView(b domain.Bill) billView {
	billType := "expense"
	if b.Type == domain.TypeIncome {
		billType = "income"
	}
	return billView{
		ID:         b.ID,
		Type:       billType,
		Amount:     b.Amount.String(),
		Currency:   b.Currency,
		CategoryID: b.CategoryID,
		Note:       b.Description,
		Date:       b.OccurredAt.UTC().Format(dateLayout),
	}
}

func toBillViews(bills []domain.Bill) []billView {
	views := make([]billView, 0, len(bills))
	for _, bill := range bills {
		views = append(views, toBillView(bill))
	}
	return views
}

func registerTools(server *mcp.Server, t *tools) {
	addTool(server, t, "transactions:write", &mcp.Tool{
		Name:        "add_bill",
		Description: "Record one expense or income.",
		Annotations: writes("Record a bill", false, false),
	}, t.addBill)

	addTool(server, t, "transactions:write", &mcp.Tool{
		Name: "add_bills",
		Description: "Record several bills atomically: if any item is rejected nothing is written and the " +
			"error names the item, e.g. items[1]. Fix that item and resend the whole batch.",
		Annotations: writes("Record several bills", false, false),
	}, t.addBills)

	addTool(server, t, "transactions:read", &mcp.Tool{
		Name:        "list_bills",
		Description: "List bills newest first, optionally filtered.",
		Annotations: readOnly("List bills"),
	}, t.listBills)

	addTool(server, t, "transactions:read", &mcp.Tool{
		Name:        "get_bill",
		Description: "Show one bill by id.",
		Annotations: readOnly("Show a bill"),
	}, t.getBill)

	addTool(server, t, "transactions:update", &mcp.Tool{
		Name:        "update_bill",
		Description: "Change some fields of one bill; fields left out keep their current value.",
		Annotations: writes("Update a bill", true, true),
	}, t.updateBill)

	addTool(server, t, "transactions:delete", &mcp.Tool{
		Name: "delete_bill",
		Description: "Delete one bill. Only after the user confirmed this exact bill (amount, date, note); " +
			"if list_bills returned several candidates, let the user choose.",
		Annotations: writes("Delete a bill", true, true),
	}, t.deleteBill)

	addTool(server, t, "categories:read", &mcp.Tool{
		Name:        "list_categories",
		Description: "Show the category tree for expense or income. Bills use the second-level names.",
		Annotations: readOnly("List categories"),
	}, t.listCategories)

	addTool(server, t, "categories:write", &mcp.Tool{
		Name: "add_category",
		Description: "Create a second-level category under an existing first-level one. Only when the user " +
			"asked for a new category; otherwise use the closest existing one.",
		Annotations: writes("Create a category", false, false),
	}, t.addCategory)

	addTool(server, t, "stats:read", &mcp.Tool{
		Name:        "get_stats",
		Description: "Spending summary for one month: monthly totals, a daily breakdown, or per-category totals.",
		Annotations: readOnly("Spending summary"),
	}, t.getStats)
}

func (t *tools) addBill(ctx context.Context, userID uuid.UUID, in billInput) (any, error) {
	input, err := in.toCreate()
	if err != nil {
		return nil, err
	}
	bill, err := t.svc.Bill.CreateBill(ctx, userID, input)
	if err != nil {
		return nil, err
	}
	return toBillView(*bill), nil
}

func (t *tools) addBills(ctx context.Context, userID uuid.UUID, in addBillsInput) (any, error) {
	inputs := make([]services.CreateBillInput, 0, len(in.Items))
	for i, item := range in.Items {
		input, err := item.toCreate()
		if err != nil {
			return nil, services.IndexBatchError(i, err)
		}
		inputs = append(inputs, input)
	}
	bills, err := t.svc.Bill.BatchCreateBills(ctx, userID, inputs)
	if err != nil {
		return nil, err
	}
	return toBillViews(bills), nil
}

func (t *tools) listBills(ctx context.Context, userID uuid.UUID, in listBillsInput) (any, error) {
	filter := services.ListBillsInput{Limit: in.Limit, Offset: in.Offset}
	if filter.Limit <= 0 {
		filter.Limit = 20
	}
	if strings.TrimSpace(in.Type) != "" {
		billType, err := parseType(in.Type)
		if err != nil {
			return nil, err
		}
		filter.Type = &billType
	}
	if name := strings.TrimSpace(in.Category); name != "" {
		categoryID, err := t.categoryIDByName(ctx, userID, name, filter.Type)
		if err != nil {
			return nil, err
		}
		filter.CategoryID = &categoryID
	}
	if currency := strings.TrimSpace(in.Currency); currency != "" {
		filter.Currency = &currency
	}
	if keyword := strings.TrimSpace(in.Keyword); keyword != "" {
		filter.Keyword = &keyword
	}
	var err error
	if filter.StartTime, err = parseOptionalDate("from", in.From); err != nil {
		return nil, err
	}
	if filter.EndTime, err = parseOptionalDate("to", in.To); err != nil {
		return nil, err
	}

	bills, err := t.svc.Bill.ListBills(ctx, userID, filter)
	if err != nil {
		return nil, err
	}
	return toBillViews(bills), nil
}

func (t *tools) getBill(ctx context.Context, userID uuid.UUID, in idInput) (any, error) {
	bill, err := t.svc.Bill.GetBill(ctx, userID, in.ID)
	if err != nil {
		return nil, err
	}
	return toBillView(*bill), nil
}

// updateBill starts from the stored row because UpdateBill replaces every
// field: passing only the changed one would blank the others.
func (t *tools) updateBill(ctx context.Context, userID uuid.UUID, in updateBillInput) (any, error) {
	existing, err := t.svc.Bill.GetBill(ctx, userID, in.ID)
	if err != nil {
		return nil, err
	}

	input := services.UpdateBillInput{
		ID:          existing.ID,
		Type:        existing.Type,
		Amount:      existing.Amount,
		Currency:    existing.Currency,
		CategoryID:  existing.CategoryID,
		Description: existing.Description,
		OccurredAt:  existing.OccurredAt,
	}
	if in.Type != nil {
		if input.Type, err = parseType(*in.Type); err != nil {
			return nil, err
		}
	}
	if in.Amount != nil {
		if input.Amount, err = parseAmount(*in.Amount); err != nil {
			return nil, err
		}
	}
	if in.Currency != nil {
		input.Currency = strings.TrimSpace(*in.Currency)
	}
	if in.Category != nil {
		input.CategoryID = 0
		input.CategoryName = strings.TrimSpace(*in.Category)
	}
	if in.Note != nil {
		input.Description = *in.Note
	}
	if in.Date != nil {
		if input.OccurredAt, err = parseDate("date", *in.Date); err != nil {
			return nil, err
		}
	}

	bill, err := t.svc.Bill.UpdateBill(ctx, userID, input)
	if err != nil {
		return nil, err
	}
	return toBillView(*bill), nil
}

func (t *tools) deleteBill(ctx context.Context, userID uuid.UUID, in idInput) (any, error) {
	if err := t.svc.Bill.DeleteBill(ctx, userID, in.ID); err != nil {
		return nil, err
	}
	return map[string]any{"deleted": true, "id": in.ID}, nil
}

func (t *tools) listCategories(ctx context.Context, userID uuid.UUID, in categoryTypeInput) (any, error) {
	billType, err := parseType(in.Type)
	if err != nil {
		return nil, err
	}
	return t.svc.Category.ListCategoriesByUser(ctx, userID, billType)
}

func (t *tools) addCategory(ctx context.Context, userID uuid.UUID, in addCategoryInput) (any, error) {
	if strings.TrimSpace(in.Parent) == "" {
		return nil, errorx.New(400, "parent is required: an existing first-level category name")
	}
	billType, err := parseType(in.Type)
	if err != nil {
		return nil, err
	}
	return t.svc.Category.CreateCategory(ctx, userID, services.CreateCategoryInput{
		Name:       in.Name,
		Type:       billType,
		ParentName: strings.TrimSpace(in.Parent),
	})
}

func (t *tools) getStats(ctx context.Context, userID uuid.UUID, in statsInput) (any, error) {
	switch strings.TrimSpace(in.Report) {
	case "monthly":
		return t.svc.Stats.GetMonthlyStats(ctx, userID, in.Month)
	case "daily":
		return t.svc.Stats.GetDailyStats(ctx, userID, in.Month)
	case "category":
		billType, err := parseType(in.Type)
		if err != nil {
			return nil, err
		}
		return t.svc.Stats.GetCategoryStats(ctx, userID, in.Month, billType)
	default:
		return nil, errorx.Newf(400, "unknown report %q: use monthly, daily or category", in.Report)
	}
}

// categoryIDByName exists because ListBills filters by id while agents work in
// names. With no type both trees are searched, and a name found in both is
// reported rather than guessed.
func (t *tools) categoryIDByName(ctx context.Context, userID uuid.UUID, name string, billType *int16) (int64, error) {
	types := []int16{domain.TypeExpense, domain.TypeIncome}
	if billType != nil {
		types = []int16{*billType}
	}

	var matches []int64
	for _, categoryType := range types {
		tree, err := t.svc.Category.ListCategoriesByUser(ctx, userID, categoryType)
		if err != nil {
			return 0, err
		}
		for _, parent := range tree {
			for _, child := range parent.Children {
				if child.Level == 2 && strings.EqualFold(strings.TrimSpace(child.Name), name) {
					matches = append(matches, child.ID)
				}
			}
		}
	}

	switch len(matches) {
	case 0:
		return 0, errorx.Newf(400, "category %q not found; call list_categories for the valid names", name)
	case 1:
		return matches[0], nil
	default:
		return 0, errorx.Newf(400, "category %q exists for both expense and income; pass a type to disambiguate", name)
	}
}

func (in billInput) toCreate() (services.CreateBillInput, error) {
	if strings.TrimSpace(in.Category) == "" {
		return services.CreateBillInput{}, errorx.New(400, "category is required (a second-level category name, e.g. 吃)")
	}
	billType, err := parseType(in.Type)
	if err != nil {
		return services.CreateBillInput{}, err
	}
	amount, err := parseAmount(in.Amount)
	if err != nil {
		return services.CreateBillInput{}, err
	}
	occurredAt, err := parseDate("date", in.Date)
	if err != nil {
		return services.CreateBillInput{}, err
	}
	return services.CreateBillInput{
		Type:         billType,
		Amount:       amount,
		Currency:     strings.TrimSpace(in.Currency),
		CategoryName: strings.TrimSpace(in.Category),
		Description:  in.Note,
		OccurredAt:   occurredAt,
	}, nil
}

// parseType maps the words the tools take onto the stored 1/2; an omitted type
// is an expense, the default everywhere.
func parseType(raw string) (int16, error) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "", "expense", "1", "支出":
		return domain.TypeExpense, nil
	case "income", "2", "收入":
		return domain.TypeIncome, nil
	default:
		return 0, errorx.Newf(400, "invalid type %q: use expense or income", raw)
	}
}

func parseAmount(raw string) (decimal.Decimal, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return decimal.Zero, errorx.New(400, "amount is required")
	}
	amount, err := decimal.NewFromString(raw)
	if err != nil {
		return decimal.Zero, errorx.Newf(400, "amount %q is not a number, e.g. 25.50", raw)
	}
	return amount, nil
}

// parseDate returns the zero time for an empty value; the bill service then
// fills in today as seen from the user's timezone.
func parseDate(field, raw string) (time.Time, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return time.Time{}, nil
	}
	parsed, err := time.Parse(dateLayout, raw)
	if err != nil {
		return time.Time{}, errorx.Newf(400, "%s %q must be YYYY-MM-DD", field, raw)
	}
	return parsed, nil
}

func parseOptionalDate(field, raw string) (*time.Time, error) {
	parsed, err := parseDate(field, raw)
	if err != nil || parsed.IsZero() {
		return nil, err
	}
	return &parsed, nil
}
