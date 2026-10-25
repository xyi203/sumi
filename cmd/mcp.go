package cmd

import (
	"context"

	"sumi/internal/apiclient"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/spf13/cobra"
)

var mcpCmd = &cobra.Command{
	Use:   "mcp",
	Short: "Serve the bill commands as an MCP server over stdio",
	Long: `Serve the bill, category, stats and import commands as MCP tools over stdio.

It is an HTTP client of the API like every other client command, and uses the
same credentials: SUMI_API_URL and SUMI_API_KEY, or what ` + "`sumi auth`" + ` stored.

  claude mcp add sumi -- sumi mcp

A running server also serves the same tools at /mcp over Streamable HTTP, minus
import_csv, which needs the CSV on the server's own disk.`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		server := apiclient.NewMCPServer(apiclient.MCPOptions{
			// Resolved per call so a key stored by `sumi auth key create` after
			// startup is picked up.
			Connect: func(context.Context, *mcp.CallToolRequest) (*apiclient.Client, error) {
				return newAPIClient()
			},
			LocalFiles: true,
		})
		// stdout carries the protocol; nothing else may write to it from here on.
		return server.Run(cmd.Context(), &mcp.StdioTransport{})
	},
}

func init() {
	rootView.AddCommand(mcpCmd)
}
