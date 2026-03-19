package mcp

import (
	"context"
	"log/slog"

	"github.com/mark3labs/mcp-go/mcp"

	"github.com/watchword/watchword/internal/service"
)

type FileHandlers struct {
	svc    *service.FileService
	logger *slog.Logger
}

func (h *FileHandlers) UploadFile(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	word := req.GetString("word", "")
	filename := req.GetString("filename", "")
	contentType := req.GetString("content_type", "")

	var ttlHours *int
	if args := req.GetArguments(); args != nil {
		if _, ok := args["ttl_hours"]; ok {
			t := req.GetInt("ttl_hours", 0)
			ttlHours = &t
		}
	}

	h.logger.Debug("upload_file called", "word", word, "filename", filename, "content_type", contentType)

	result, err := h.svc.UploadFile(ctx, word, filename, contentType, ttlHours)
	if err != nil {
		h.logger.Warn("upload_file failed", "word", word, "error", err)
		return mcp.NewToolResultError(err.Error()), nil
	}

	h.logger.Info("upload_file success", "id", result.ID, "word", result.Word, "filename", result.Filename)

	return marshalResult(result)
}

func (h *FileHandlers) DownloadFile(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	word := req.GetString("word", "")

	h.logger.Debug("download_file called", "word", word)

	result, err := h.svc.DownloadFile(ctx, word)
	if err != nil {
		h.logger.Warn("download_file failed", "word", word, "error", err)
		return mcp.NewToolResultError(err.Error()), nil
	}

	h.logger.Info("download_file success", "word", word, "filename", result.Filename)

	return marshalResult(result)
}
