// Package mcpserver exposes the bill, category and stats operations as MCP
// tools. Like the HTTP handlers it is a transport over the service layer: tools
// call services directly and see only domain types.
package mcpserver

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"runtime/debug"
	"strings"

	"sumi/internal/services"
	"sumi/pkg/errorx"
	"sumi/pkg/logx"

	"github.com/google/uuid"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// instructions is what a connected agent reads before its first call: the
// conventions that tool descriptions cannot carry alone.
const instructions = `sumi records and queries the user's personal bills.

- Categories are two-level and a bill can only use a second-level name (e.g. 吃, 行),
  never a first-level one (e.g. 必要). Call list_categories when unsure.
- Omit date unless the user named a day: the server then records today in the
  user's own timezone, which your clock may not match. Omit currency unless the
  user named one.
- Several bills in one request go through add_bills, which is all or nothing.
- Confirm with the user before delete_bill.
- A 403 means the API key lacks a scope; tell the user instead of retrying.
- Reply to the user in plain language, not raw JSON.`

// New builds the MCP server. It holds no per-caller state: every tool call
// authenticates itself from the headers of the request carrying it.
func New(svc *services.Service) *mcp.Server {
	server := mcp.NewServer(
		&mcp.Implementation{Name: "sumi", Title: "sumi 记账", Version: buildVersion()},
		&mcp.ServerOptions{Instructions: instructions},
	)
	registerTools(server, &tools{svc: svc})
	return server
}

type tools struct {
	svc *services.Service
}

// addTool registers a tool together with the API key scope it needs, the same
// scope as the REST route doing the same thing, so a tool cannot be added
// without deciding who may call it.
func addTool[In any](server *mcp.Server, t *tools, scope string, tool *mcp.Tool,
	handle func(context.Context, uuid.UUID, In) (any, error)) {
	mcp.AddTool(server, tool, func(ctx context.Context, req *mcp.CallToolRequest, in In) (*mcp.CallToolResult, any, error) {
		userID, err := t.authorize(ctx, req, scope)
		if err != nil {
			return nil, nil, toolError(err)
		}
		out, err := handle(ctx, userID, in)
		if err != nil {
			return nil, nil, toolError(err)
		}
		encoded, err := json.Marshal(out)
		if err != nil {
			return nil, nil, toolError(err)
		}
		return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: string(encoded)}}}, nil, nil
	})
}

// authorize resolves the caller of one tool call. The SDK does not hand a tool
// the HTTP request's context, so the identity middleware.JWTOrAPIKey put in
// Fiber's Locals is out of reach; the credential is read again from the
// headers, with the same precedence: a bearer token wins and carries every
// scope, an API key needs the tool's scope.
func (t *tools) authorize(ctx context.Context, req *mcp.CallToolRequest, scope string) (uuid.UUID, error) {
	if req.Extra == nil {
		return uuid.Nil, errorx.ErrUnauthorized
	}

	if authorization := req.Extra.Header.Get("Authorization"); authorization != "" {
		token, ok := strings.CutPrefix(authorization, "Bearer ")
		if !ok {
			return uuid.Nil, errorx.ErrUnauthorized
		}
		claims, err := t.svc.Auth.ParseAccessToken(strings.TrimSpace(token))
		if err != nil {
			return uuid.Nil, err
		}
		userID, err := uuid.Parse(claims.UserID)
		if err != nil {
			return uuid.Nil, errorx.ErrUnauthorized
		}
		return userID, nil
	}

	identity, err := t.svc.APIKey.Validate(ctx, req.Extra.Header.Get("X-API-Key"), []string{scope})
	if err != nil {
		return uuid.Nil, err
	}
	return identity.UserID, nil
}

// toolError gives the agent the status and message the REST API would have
// sent. Anything that is not an errorx.Error is internal and only logged, as
// middleware.ErrorHandler does, so database errors never reach the agent.
func toolError(err error) error {
	var typed *errorx.Error
	if errors.As(err, &typed) {
		return fmt.Errorf("%d: %s", typed.Code, typed.Message)
	}
	logx.WithError(err).Error("mcp tool call failed")
	return errors.New("500: Internal Server Error")
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
