package apiclient

import (
	"context"
	"encoding/json"
	"runtime/debug"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// mcpInstructions is what a connected agent reads before its first call: the
// conventions that tool descriptions cannot carry alone.
const mcpInstructions = `sumi records and queries the user's personal bills.

- Categories are two-level and a bill can only use a second-level name (e.g. 吃, 行),
  never a first-level one (e.g. 必要). Call list_categories when unsure.
- Omit date unless the user named a day: the server then records today in the
  user's own timezone, which your clock may not match. Omit currency unless the
  user named one.
- Several bills in one request go through add_bills, which is all or nothing.
- Confirm with the user before delete_bill.
- A 403 means the API key lacks a scope; tell the user instead of retrying.
- Reply to the user in plain language, not raw JSON.`

// MCPOptions decides where a tool call's requests go and with which credential.
type MCPOptions struct {
	// Connect returns the client for one tool call. It runs per call, so each
	// call uses the credential of the request being served, and a missing one
	// surfaces as a tool error the agent can relay.
	Connect func(ctx context.Context, req *mcp.CallToolRequest) (*Client, error)
}

type idInput struct {
	ID int64 `json:"id" jsonschema:"transaction id, from list_bills"`
}

type addBillsInput struct {
	Items []BillInput `json:"items" jsonschema:"transactions to insert in one transaction, at most 1000"`
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

// rawResult hands the API's JSON to the agent unchanged.
func rawResult(raw []byte, err error) (*mcp.CallToolResult, any, error) {
	if err != nil {
		return nil, nil, err
	}
	text := strings.TrimSpace(string(raw))
	if text == "" {
		text = "{}"
	}
	return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: text}}}, nil, nil
}

func valueResult(value any, err error) (*mcp.CallToolResult, any, error) {
	if err != nil {
		return nil, nil, err
	}
	encoded, err := json.Marshal(value)
	return rawResult(encoded, err)
}

func readOnly(title string) *mcp.ToolAnnotations {
	return &mcp.ToolAnnotations{Title: title, ReadOnlyHint: true}
}

func writes(title string, destructive, idempotent bool) *mcp.ToolAnnotations {
	return &mcp.ToolAnnotations{Title: title, DestructiveHint: &destructive, IdempotentHint: idempotent}
}

func buildVersion() string {
	if info, ok := debug.ReadBuildInfo(); ok && info.Main.Version != "" && info.Main.Version != "(devel)" {
		return info.Main.Version
	}
	return "dev"
}

// NewMCPServer exposes the bill, category and stats operations as MCP tools.
func NewMCPServer(opts MCPOptions) *mcp.Server {
	server := mcp.NewServer(
		&mcp.Implementation{Name: "sumi", Title: "sumi 记账", Version: buildVersion()},
		&mcp.ServerOptions{Instructions: mcpInstructions},
	)

	addTool(server, &mcp.Tool{
		Name:        "add_bill",
		Description: "Record one expense or income.",
		Annotations: writes("Record a bill", false, false),
	}, opts, func(ctx context.Context, c *Client, in BillInput) (*mcp.CallToolResult, any, error) {
		return rawResult(c.AddBill(ctx, in))
	})

	addTool(server, &mcp.Tool{
		Name: "add_bills",
		Description: "Record several bills atomically: if any item is rejected nothing is written and the " +
			"error names the item, e.g. items[1]. Fix that item and resend the whole batch.",
		Annotations: writes("Record several bills", false, false),
	}, opts, func(ctx context.Context, c *Client, in addBillsInput) (*mcp.CallToolResult, any, error) {
		return rawResult(c.AddBills(ctx, in.Items))
	})

	addTool(server, &mcp.Tool{
		Name:        "list_bills",
		Description: "List bills newest first, optionally filtered.",
		Annotations: readOnly("List bills"),
	}, opts, func(ctx context.Context, c *Client, in listBillsInput) (*mcp.CallToolResult, any, error) {
		if in.Limit <= 0 {
			in.Limit = 20
		}
		return rawResult(c.ListBills(ctx, BillFilter(in)))
	})

	addTool(server, &mcp.Tool{
		Name:        "get_bill",
		Description: "Show one bill by id.",
		Annotations: readOnly("Show a bill"),
	}, opts, func(ctx context.Context, c *Client, in idInput) (*mcp.CallToolResult, any, error) {
		return rawResult(c.GetBill(ctx, in.ID))
	})

	addTool(server, &mcp.Tool{
		Name:        "update_bill",
		Description: "Change some fields of one bill; fields left out keep their current value.",
		Annotations: writes("Update a bill", true, true),
	}, opts, func(ctx context.Context, c *Client, in updateBillInput) (*mcp.CallToolResult, any, error) {
		return rawResult(c.UpdateBill(ctx, in.ID, BillPatch{
			Amount:   in.Amount,
			Category: in.Category,
			Type:     in.Type,
			Note:     in.Note,
			Date:     in.Date,
			Currency: in.Currency,
		}))
	})

	addTool(server, &mcp.Tool{
		Name: "delete_bill",
		Description: "Delete one bill. Only after the user confirmed this exact bill (amount, date, note); " +
			"if list_bills returned several candidates, let the user choose.",
		Annotations: writes("Delete a bill", true, true),
	}, opts, func(ctx context.Context, c *Client, in idInput) (*mcp.CallToolResult, any, error) {
		return valueResult(c.DeleteBill(ctx, in.ID))
	})

	addTool(server, &mcp.Tool{
		Name:        "list_categories",
		Description: "Show the category tree for expense or income. Bills use the second-level names.",
		Annotations: readOnly("List categories"),
	}, opts, func(ctx context.Context, c *Client, in categoryTypeInput) (*mcp.CallToolResult, any, error) {
		return rawResult(c.ListCategories(ctx, orExpense(in.Type)))
	})

	addTool(server, &mcp.Tool{
		Name: "add_category",
		Description: "Create a second-level category under an existing first-level one. Only when the user " +
			"asked for a new category; otherwise use the closest existing one.",
		Annotations: writes("Create a category", false, false),
	}, opts, func(ctx context.Context, c *Client, in addCategoryInput) (*mcp.CallToolResult, any, error) {
		return rawResult(c.AddCategory(ctx, in.Name, in.Parent, orExpense(in.Type)))
	})

	addTool(server, &mcp.Tool{
		Name:        "get_stats",
		Description: "Spending summary for one month: monthly totals, a daily breakdown, or per-category totals.",
		Annotations: readOnly("Spending summary"),
	}, opts, func(ctx context.Context, c *Client, in statsInput) (*mcp.CallToolResult, any, error) {
		return rawResult(c.GetStats(ctx, strings.TrimSpace(in.Report), in.Month, in.Type))
	})

	return server
}

// addTool registers a tool whose handler receives the call's client, resolved
// before the handler runs so every tool reports a missing credential the same way.
func addTool[In any](server *mcp.Server, tool *mcp.Tool, opts MCPOptions,
	handle func(context.Context, *Client, In) (*mcp.CallToolResult, any, error)) {
	mcp.AddTool(server, tool, func(ctx context.Context, req *mcp.CallToolRequest, in In) (*mcp.CallToolResult, any, error) {
		client, err := opts.Connect(ctx, req)
		if err != nil {
			return nil, nil, err
		}
		return handle(ctx, client, in)
	})
}
