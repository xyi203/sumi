package mcpserver

import (
	"context"
	"strings"
	"testing"

	"sumi/internal/services"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// connect starts the server in memory. No service is backed by a database, so
// only paths that stop before reaching one can be driven here; everything past
// authorization is covered by internal/apps/mcp_test.go.
func connect(t *testing.T) *mcp.ClientSession {
	t.Helper()

	ctx := context.Background()
	serverTransport, clientTransport := mcp.NewInMemoryTransports()
	serverSession, err := New(&services.Service{}).Connect(ctx, serverTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { serverSession.Close() })

	client := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "0"}, nil)
	session, err := client.Connect(ctx, clientTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { session.Close() })
	return session
}

func TestListsEveryTool(t *testing.T) {
	listed, err := connect(t).ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	tools := map[string]*mcp.Tool{}
	for _, tool := range listed.Tools {
		tools[tool.Name] = tool
	}

	for _, name := range []string{
		"add_bill", "add_bills", "list_bills", "get_bill", "update_bill", "delete_bill",
		"list_categories", "add_category", "get_stats",
	} {
		if tools[name] == nil {
			t.Errorf("tool %q is not listed", name)
		}
	}
	if len(tools) != 9 {
		t.Errorf("got %d tools, want 9", len(tools))
	}

	// A client may skip confirmation for tools not marked destructive.
	del := tools["delete_bill"].Annotations
	if del == nil || del.DestructiveHint == nil || !*del.DestructiveHint {
		t.Error("delete_bill must be annotated destructive")
	}
	if list := tools["list_bills"].Annotations; list == nil || !list.ReadOnlyHint {
		t.Error("list_bills must be annotated read-only")
	}
}

// A call that arrives without HTTP headers has no credential, and must fail
// closed instead of reaching a service.
func TestCallWithoutCredentialIsUnauthorized(t *testing.T) {
	result, err := connect(t).CallTool(context.Background(), &mcp.CallToolParams{
		Name: "list_bills", Arguments: map[string]any{},
	})
	if err != nil {
		t.Fatal(err)
	}
	text := result.Content[0].(*mcp.TextContent).Text
	if !result.IsError || !strings.Contains(text, "401") {
		t.Fatalf("want a 401 tool error, got isError=%v %q", result.IsError, text)
	}
}

func TestParseType(t *testing.T) {
	for raw, want := range map[string]int16{"": 1, "expense": 1, " Income ": 2, "支出": 1, "2": 2} {
		if got, err := parseType(raw); err != nil || got != want {
			t.Errorf("parseType(%q) = %d, %v; want %d", raw, got, err, want)
		}
	}
	if _, err := parseType("refund"); err == nil {
		t.Error("parseType must reject an unknown type")
	}
}

func TestBillInputRejectsBadFields(t *testing.T) {
	for name, in := range map[string]billInput{
		"missing category": {Amount: "1"},
		"missing amount":   {Category: "吃"},
		"bad amount":       {Amount: "ten", Category: "吃"},
		"bad date":         {Amount: "1", Category: "吃", Date: "2026/08/01"},
		"bad type":         {Amount: "1", Category: "吃", Type: "refund"},
	} {
		if _, err := in.toCreate(); err == nil {
			t.Errorf("%s: want an error", name)
		}
	}

	input, err := billInput{Amount: " 25.50 ", Category: " 吃 ", Date: "2026-08-01"}.toCreate()
	if err != nil {
		t.Fatal(err)
	}
	if input.Amount.String() != "25.5" || input.CategoryName != "吃" || input.Type != 1 ||
		input.OccurredAt.Format(dateLayout) != "2026-08-01" {
		t.Errorf("unexpected input %+v", input)
	}
}
