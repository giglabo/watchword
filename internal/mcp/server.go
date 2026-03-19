package mcp

import (
	"log/slog"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"

	"github.com/watchword/watchword/internal/config"
	"github.com/watchword/watchword/internal/service"
)

// propTypes maps each tool's property names to their JSON Schema types.
var propTypes = map[string]map[string]string{
	"store_entry": {
		"word": "string", "payload": "string", "ttl_hours": "integer",
	},
	"get_entry": {
		"id": "string",
	},
	"get_entry_by_word": {
		"word": "string", "include_expired": "boolean",
	},
	"search_entries": {
		"pattern": "string", "status": "string", "limit": "integer", "offset": "integer",
	},
	"restore_entry": {
		"id": "string", "new_ttl_hours": "integer",
	},
	"list_entries": {
		"status": "string", "limit": "integer", "offset": "integer",
		"sort_by": "string", "sort_order": "string",
	},
	"delete_entry": {
		"id": "string",
	},
	"upload_file": {
		"word": "string", "filename": "string", "content_type": "string", "ttl_hours": "integer",
	},
	"download_file": {
		"word": "string",
	},
}

func buildProps(toolName string, descs map[string]string) map[string]any {
	types := propTypes[toolName]
	props := make(map[string]any, len(types))
	for name, typ := range types {
		desc := descs[name]
		props[name] = map[string]interface{}{"type": typ, "description": desc}
	}
	return props
}

func NewServer(svc *service.EntryService, fileSvc *service.FileService, tools config.ToolsConfig, logger *slog.Logger) *server.MCPServer {
	s := server.NewMCPServer(
		"watchword",
		"1.0.0",
		server.WithLogging(),
	)

	h := &Handlers{svc: svc, logger: logger}

	s.AddTool(mcp.Tool{
		Name:        "store_entry",
		Description: tools.StoreEntry.Description,
		InputSchema: mcp.ToolInputSchema{
			Type:       "object",
			Properties: buildProps("store_entry", tools.StoreEntry.Properties),
			Required:   []string{"word", "payload"},
		},
	}, h.StoreEntry)

	s.AddTool(mcp.Tool{
		Name:        "get_entry",
		Description: tools.GetEntry.Description,
		InputSchema: mcp.ToolInputSchema{
			Type:       "object",
			Properties: buildProps("get_entry", tools.GetEntry.Properties),
			Required:   []string{"id"},
		},
	}, h.GetEntry)

	s.AddTool(mcp.Tool{
		Name:        "get_entry_by_word",
		Description: tools.GetEntryByWord.Description,
		InputSchema: mcp.ToolInputSchema{
			Type:       "object",
			Properties: buildProps("get_entry_by_word", tools.GetEntryByWord.Properties),
			Required:   []string{"word"},
		},
	}, h.GetEntryByWord)

	s.AddTool(mcp.Tool{
		Name:        "search_entries",
		Description: tools.SearchEntries.Description,
		InputSchema: mcp.ToolInputSchema{
			Type:       "object",
			Properties: buildProps("search_entries", tools.SearchEntries.Properties),
			Required:   []string{"pattern"},
		},
	}, h.SearchEntries)

	s.AddTool(mcp.Tool{
		Name:        "restore_entry",
		Description: tools.RestoreEntry.Description,
		InputSchema: mcp.ToolInputSchema{
			Type:       "object",
			Properties: buildProps("restore_entry", tools.RestoreEntry.Properties),
			Required:   []string{"id"},
		},
	}, h.RestoreEntry)

	s.AddTool(mcp.Tool{
		Name:        "list_entries",
		Description: tools.ListEntries.Description,
		InputSchema: mcp.ToolInputSchema{
			Type:       "object",
			Properties: buildProps("list_entries", tools.ListEntries.Properties),
		},
	}, h.ListEntries)

	s.AddTool(mcp.Tool{
		Name:        "delete_entry",
		Description: tools.DeleteEntry.Description,
		InputSchema: mcp.ToolInputSchema{
			Type:       "object",
			Properties: buildProps("delete_entry", tools.DeleteEntry.Properties),
			Required:   []string{"id"},
		},
	}, h.DeleteEntry)

	// Conditionally register file tools when S3 is configured
	if fileSvc != nil {
		fh := &FileHandlers{svc: fileSvc, logger: logger}

		s.AddTool(mcp.Tool{
			Name:        "upload_file",
			Description: tools.UploadFile.Description,
			InputSchema: mcp.ToolInputSchema{
				Type:       "object",
				Properties: buildProps("upload_file", tools.UploadFile.Properties),
				Required:   []string{"word", "filename"},
			},
		}, fh.UploadFile)

		s.AddTool(mcp.Tool{
			Name:        "download_file",
			Description: tools.DownloadFile.Description,
			InputSchema: mcp.ToolInputSchema{
				Type:       "object",
				Properties: buildProps("download_file", tools.DownloadFile.Properties),
				Required:   []string{"word"},
			},
		}, fh.DownloadFile)
	}

	return s
}
