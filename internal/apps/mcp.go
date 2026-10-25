package apps

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"time"

	"sumi/internal/apiclient"

	"github.com/gofiber/fiber/v3"
	"github.com/gofiber/fiber/v3/middleware/adaptor"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// inProcessBaseURL only has to parse: requests to it never leave the process.
const inProcessBaseURL = "http://sumi.internal"

// newMCPHandler serves the MCP tools over Streamable HTTP. The tools are the
// same apiclient code `sumi mcp` runs, pointed at this app in-process and
// carrying the caller's own credential, so every tool call passes through the
// same routes, auth and scope checks as a direct API call: a key without
// transactions:delete cannot delete through MCP either.
//
// import_csv is left out: its file path would name a file on this server.
func newMCPHandler(app *fiber.App) fiber.Handler {
	inProcess := &http.Client{
		Transport: inProcessTransport{handler: adaptor.FiberApp(app)},
		Timeout:   30 * time.Second,
	}

	server := apiclient.NewMCPServer(apiclient.MCPOptions{
		Connect: func(_ context.Context, req *mcp.CallToolRequest) (*apiclient.Client, error) {
			client := &apiclient.Client{BaseURL: inProcessBaseURL, HTTP: inProcess}
			if req.Extra != nil {
				// Same precedence as middleware.JWTOrAPIKey: a bearer token wins.
				if bearer, ok := strings.CutPrefix(req.Extra.Header.Get("Authorization"), "Bearer "); ok {
					client.Bearer = strings.TrimSpace(bearer)
				} else {
					client.APIKey = strings.TrimSpace(req.Extra.Header.Get("X-API-Key"))
				}
			}
			if client.APIKey == "" && client.Bearer == "" {
				// The route's auth middleware rejects this first; kept so a
				// misconfigured route fails closed rather than calling anonymously.
				return nil, errors.New("no credential: send X-API-Key or Authorization: Bearer")
			}
			return client, nil
		},
	})

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

// inProcessTransport hands a request straight to the Fiber app instead of a
// socket, so the MCP endpoint needs neither its own listen address nor a
// loopback hop.
type inProcessTransport struct {
	handler http.Handler
}

func (t inProcessTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	req = req.Clone(req.Context())
	// Server-side fields a client request leaves empty; the adaptor reads them.
	req.RequestURI = req.URL.RequestURI()
	req.RemoteAddr = "127.0.0.1:0"

	recorder := httptest.NewRecorder()
	t.handler.ServeHTTP(recorder, req)
	return recorder.Result(), nil
}
