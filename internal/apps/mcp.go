package apps

import (
	"net/http"

	"sumi/internal/mcpserver"
	"sumi/internal/services"

	"github.com/gofiber/fiber/v3"
	"github.com/gofiber/fiber/v3/middleware/adaptor"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// newMCPHandler serves the MCP tools over Streamable HTTP.
func newMCPHandler(svc *services.Service) fiber.Handler {
	server := mcpserver.New(svc)
	handler := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return server }, &mcp.StreamableHTTPOptions{
		// Every call carries its own credential, so there is no session state to
		// keep, and plain JSON responses pass through the fasthttp adaptor intact.
		Stateless:    true,
		JSONResponse: true,
		// The protection targets unauthenticated local servers reached through
		// DNS rebinding. This route requires a credential a rebinding page cannot
		// attach, while the check would reject every request from a reverse proxy
		// on the same host, which forwards the public Host header.
		DisableLocalhostProtection: true,
	})
	return adaptor.HTTPHandler(handler)
}
