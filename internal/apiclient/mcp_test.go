package apiclient

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type recordedRequest struct {
	Method string
	Path   string
	Query  string
	APIKey string
	Body   map[string]any
}

// fakeAPI answers like the real API for the few routes the tests touch and
// records every request, so a test can assert what the MCP tools actually sent.
type fakeAPI struct {
	mu       sync.Mutex
	requests []recordedRequest
}

func (f *fakeAPI) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	raw, _ := io.ReadAll(r.Body)
	var body map[string]any
	_ = json.Unmarshal(raw, &body)

	f.mu.Lock()
	f.requests = append(f.requests, recordedRequest{
		Method: r.Method, Path: r.URL.Path, Query: r.URL.RawQuery,
		APIKey: r.Header.Get("X-API-Key"), Body: body,
	})
	f.mu.Unlock()

	w.Header().Set("Content-Type", "application/json")
	switch {
	case r.Method == "GET" && r.URL.Path == "/api/transactions/7":
		io.WriteString(w, `{"id":7,"type":1,"amount":"25.50","currency":"CNY","category_id":3,"description":"午饭","occurred_at":"2026-08-01"}`)
	case r.Method == "PUT" && r.URL.Path == "/api/transactions/7":
		w.Write(raw)
	case r.Method == "GET" && r.URL.Path == "/api/stats/category":
		io.WriteString(w, `[]`)
	case r.Method == "DELETE" && r.URL.Path == "/api/transactions/404":
		w.WriteHeader(http.StatusNotFound)
		io.WriteString(w, `{"success":false,"code":404,"message":"Transaction not found"}`)
	default:
		w.WriteHeader(http.StatusNotImplemented)
		io.WriteString(w, `{"message":"unexpected request in test"}`)
	}
}

func (f *fakeAPI) recorded() []recordedRequest {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]recordedRequest(nil), f.requests...)
}

// connectMCP starts the server in memory, with every tool call going to a fake
// API, and returns a connected client session. apiKey "" simulates a caller with
// no credential.
func connectMCP(t *testing.T, apiKey string) (*mcp.ClientSession, *fakeAPI) {
	t.Helper()

	api := &fakeAPI{}
	httpServer := httptest.NewServer(api)
	t.Cleanup(httpServer.Close)

	server := NewMCPServer(MCPOptions{
		Connect: func(context.Context, *mcp.CallToolRequest) (*Client, error) {
			if apiKey == "" {
				return nil, errors.New("no API key")
			}
			return &Client{BaseURL: httpServer.URL, APIKey: apiKey, HTTP: httpServer.Client()}, nil
		},
		LocalFiles: true,
	})

	ctx := context.Background()
	serverTransport, clientTransport := mcp.NewInMemoryTransports()
	serverSession, err := server.Connect(ctx, serverTransport, nil)
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
	return session, api
}

func callTool(t *testing.T, session *mcp.ClientSession, name string, args map[string]any) (string, bool) {
	t.Helper()
	result, err := session.CallTool(context.Background(), &mcp.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		t.Fatalf("%s: protocol error: %v", name, err)
	}
	var text strings.Builder
	for _, content := range result.Content {
		if tc, ok := content.(*mcp.TextContent); ok {
			text.WriteString(tc.Text)
		}
	}
	return text.String(), result.IsError
}

func TestMCPListsEveryTool(t *testing.T) {
	session, _ := connectMCP(t, "sk_test")

	listed, err := session.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	tools := map[string]*mcp.Tool{}
	for _, tool := range listed.Tools {
		tools[tool.Name] = tool
	}

	for _, name := range []string{
		"add_bill", "add_bills", "list_bills", "get_bill", "update_bill", "delete_bill",
		"list_categories", "add_category", "get_stats", "import_csv",
	} {
		if tools[name] == nil {
			t.Errorf("tool %q is not listed", name)
		}
	}
	if len(tools) != 10 {
		t.Errorf("got %d tools, want 10", len(tools))
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

func TestMCPUpdateKeepsUnchangedFields(t *testing.T) {
	session, api := connectMCP(t, "sk_test")

	if text, isErr := callTool(t, session, "update_bill", map[string]any{"id": 7, "amount": "31.00"}); isErr {
		t.Fatalf("update_bill failed: %s", text)
	}

	requests := api.recorded()
	if len(requests) != 2 || requests[0].Method != "GET" || requests[1].Method != "PUT" {
		t.Fatalf("want GET then PUT, got %+v", requests)
	}
	put := requests[1]
	if put.APIKey != "sk_test" {
		t.Errorf("PUT sent X-API-Key %q", put.APIKey)
	}
	// The PUT is a full replace, so every field not being changed must be resent.
	want := map[string]any{
		"amount": "31.00", "description": "午饭", "currency": "CNY",
		"occurred_at": "2026-08-01", "category_id": float64(3), "type": float64(1),
	}
	for field, value := range want {
		if put.Body[field] != value {
			t.Errorf("PUT %s = %v, want %v", field, put.Body[field], value)
		}
	}
}

func TestMCPBatchRejectsBadItemBeforeSending(t *testing.T) {
	session, api := connectMCP(t, "sk_test")

	text, isErr := callTool(t, session, "add_bills", map[string]any{"items": []map[string]any{
		{"amount": "12", "category": "吃"},
		{"amount": "30", "category": "行", "type": "refund"},
	}})
	if !isErr || !strings.Contains(text, "items[1]") {
		t.Fatalf("want a tool error naming items[1], got isError=%v %q", isErr, text)
	}
	if n := len(api.recorded()); n != 0 {
		t.Errorf("a rejected batch must not reach the API, sent %d requests", n)
	}
}

func TestMCPReportsServerErrorsAsToolErrors(t *testing.T) {
	session, _ := connectMCP(t, "sk_test")

	text, isErr := callTool(t, session, "delete_bill", map[string]any{"id": 404})
	if !isErr || !strings.Contains(text, "Transaction not found") {
		t.Fatalf("want the server's message as a tool error, got isError=%v %q", isErr, text)
	}
}

func TestMCPStatsDefaultsCategoryReportToExpense(t *testing.T) {
	session, api := connectMCP(t, "sk_test")

	if text, isErr := callTool(t, session, "get_stats", map[string]any{"report": "category", "month": "2026-08"}); isErr {
		t.Fatalf("get_stats failed: %s", text)
	}
	requests := api.recorded()
	if len(requests) != 1 || requests[0].Query != "month=2026-08&type=1" {
		t.Fatalf("unexpected requests %+v", requests)
	}

	if text, isErr := callTool(t, session, "get_stats", map[string]any{"report": "yearly"}); !isErr {
		t.Fatalf("an unknown report must fail, got %q", text)
	}
}

func TestMCPWithoutCredentialFailsPerCallAndDryRunStillWorks(t *testing.T) {
	session, _ := connectMCP(t, "")

	text, isErr := callTool(t, session, "list_bills", map[string]any{})
	if !isErr || !strings.Contains(text, "no API key") {
		t.Fatalf("want a missing-key tool error, got isError=%v %q", isErr, text)
	}

	csvPath := filepath.Join(t.TempDir(), "bill.csv")
	csv := "date,amount,category,note\n2026-08-01,12.00,吃,早饭\n2026-08-02,0,吃,不计\n"
	if err := os.WriteFile(csvPath, []byte(csv), 0o600); err != nil {
		t.Fatal(err)
	}
	text, isErr = callTool(t, session, "import_csv", map[string]any{"file": csvPath, "dry_run": true})
	if isErr {
		t.Fatalf("dry run needs no credential, got %q", text)
	}
	var preview map[string]any
	if err := json.Unmarshal([]byte(text), &preview); err != nil {
		t.Fatal(err)
	}
	if preview["would_import"] != float64(1) {
		t.Errorf("would_import = %v, want 1 (the zero-amount row is dropped)", preview["would_import"])
	}
}
