package apps

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"strconv"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v3"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// listen serves the test app on a real socket: /mcp goes through the fasthttp
// adaptor, which app.Test would not exercise the way production does.
func (e *testEnv) listen() string {
	e.t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		e.t.Fatal(err)
	}
	go func() { _ = e.app.Listener(ln, fiber.ListenConfig{DisableStartupMessage: true}) }()
	e.t.Cleanup(func() { _ = e.app.Shutdown() })
	return "http://" + ln.Addr().String()
}

type headerTransport struct {
	header http.Header
}

func (t headerTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	req = req.Clone(req.Context())
	for key, values := range t.header {
		req.Header[key] = values
	}
	return http.DefaultTransport.RoundTrip(req)
}

func (e *testEnv) connectMCP(baseURL, apiKey string) (*mcp.ClientSession, error) {
	transport := &mcp.StreamableClientTransport{
		Endpoint: baseURL + "/mcp",
		HTTPClient: &http.Client{Transport: headerTransport{header: http.Header{
			"X-Api-Key": []string{apiKey},
		}}},
		MaxRetries:           -1,
		DisableStandaloneSSE: true,
	}
	client := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "0"}, nil)
	session, err := client.Connect(e.t.Context(), transport, nil)
	if err == nil {
		e.t.Cleanup(func() { session.Close() })
	}
	return session, err
}

func callMCPTool(t *testing.T, session *mcp.ClientSession, name string, args map[string]any) (string, bool) {
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

// TestMCPEndpointRoundTrip drives /mcp with a real MCP client: a bill recorded
// through a tool must land in the database and be visible to the plain API.
func TestMCPEndpointRoundTrip(t *testing.T) {
	env := newTestEnv(t)
	session, err := env.connectMCP(env.listen(), env.key)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}

	listed, err := session.ListTools(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(listed.Tools) != 9 {
		t.Errorf("got %d tools, want 9", len(listed.Tools))
	}

	text, isErr := callMCPTool(t, session, "add_bill", map[string]any{
		"amount": "18.80", "category": "吃", "note": "mcp-roundtrip", "date": "2026-08-07",
	})
	if isErr {
		t.Fatalf("add_bill: %s", text)
	}
	var created billBody
	if err := json.Unmarshal([]byte(text), &created); err != nil {
		t.Fatalf("add_bill returned %q: %v", text, err)
	}

	var listedBills []billBody
	env.mustDo("GET", "/api/transactions/?keyword=mcp-roundtrip", "key", nil, http.StatusOK, &listedBills)
	if len(listedBills) != 1 || listedBills[0].ID != created.ID || listedBills[0].Amount != "18.8" {
		t.Fatalf("bill recorded over MCP not found through the API: %+v", listedBills)
	}

	text, isErr = callMCPTool(t, session, "update_bill", map[string]any{"id": created.ID, "amount": "20"})
	if isErr {
		t.Fatalf("update_bill: %s", text)
	}
	var updated billBody
	env.mustDo("GET", "/api/transactions/"+strconv.FormatInt(created.ID, 10), "key", nil, http.StatusOK, &updated)
	if updated.Amount != "20" || updated.Description != "mcp-roundtrip" {
		t.Fatalf("update over MCP must change the amount and keep the note: %+v", updated)
	}
}

// TestMCPEndpointEnforcesAuthAndScopes: connecting needs a credential, and each
// tool call is held to the scope its route requires.
func TestMCPEndpointEnforcesAuthAndScopes(t *testing.T) {
	env := newTestEnv(t)
	baseURL := env.listen()

	if _, err := env.connectMCP(baseURL, ""); err == nil {
		t.Fatal("connecting without a credential must fail")
	}
	if _, err := env.connectMCP(baseURL, "sk_not_a_real_key"); err == nil {
		t.Fatal("connecting with an unknown key must fail")
	}

	var readOnly struct {
		Key string `json:"key"`
	}
	env.mustDo("POST", "/api/api-keys/", "jwt", map[string]any{
		"name": "mcp-read-only", "scopes": []string{"transactions:read"},
	}, http.StatusCreated, &readOnly)

	session, err := env.connectMCP(baseURL, readOnly.Key)
	if err != nil {
		t.Fatalf("a valid key with any scope may connect: %v", err)
	}
	if text, isErr := callMCPTool(t, session, "list_bills", map[string]any{}); isErr {
		t.Fatalf("read scope should allow list_bills: %s", text)
	}
	text, isErr := callMCPTool(t, session, "add_bill", map[string]any{"amount": "1", "category": "吃"})
	if !isErr || !strings.Contains(text, "403") {
		t.Fatalf("add_bill without transactions:write must be a 403 tool error, got isError=%v %q", isErr, text)
	}
}
