package mcp

import (
	"context"
	"encoding/json"
	"log/slog"

	"github.com/mark3labs/mcp-go/mcp"

	"github.com/watchword/watchword/internal/domain"
	"github.com/watchword/watchword/internal/service"
)

type Handlers struct {
	svc    *service.EntryService
	logger *slog.Logger
}

func (h *Handlers) StoreEntry(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	word := req.GetString("word", "")
	payload := req.GetString("payload", "")

	var ttlHours *int
	if args := req.GetArguments(); args != nil {
		if _, ok := args["ttl_hours"]; ok {
			t := req.GetInt("ttl_hours", 0)
			ttlHours = &t
		}
	}

	h.logger.Debug("store_entry called", "word", word, "payload_len", len(payload), "ttl_hours", ttlHours)

	result, err := h.svc.StoreEntry(ctx, word, payload, ttlHours)
	if err != nil {
		h.logger.Warn("store_entry failed", "word", word, "error", err)
		return mcp.NewToolResultError(err.Error()), nil
	}

	h.logger.Info("store_entry success", "id", result.Entry.ID, "word", result.Entry.Word, "collision", result.CollisionResolved)

	resp := map[string]interface{}{
		"id":                 result.Entry.ID.String(),
		"word":               result.Entry.Word,
		"status":             result.Entry.Status,
		"collision_resolved": result.CollisionResolved,
	}
	if result.Entry.ExpiresAt != nil {
		resp["expires_at"] = result.Entry.ExpiresAt
	}
	if result.CollisionResolved {
		resp["original_word"] = result.OriginalWord
	}

	return marshalResult(resp)
}

func (h *Handlers) GetEntry(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	id := req.GetString("id", "")

	h.logger.Debug("get_entry called", "id", id)

	entry, err := h.svc.GetEntry(ctx, id)
	if err != nil {
		h.logger.Warn("get_entry failed", "id", id, "error", err)
		return mcp.NewToolResultError(err.Error()), nil
	}

	h.logger.Debug("get_entry success", "id", id, "word", entry.Word, "status", entry.Status)

	if entry.EntryType == domain.EntryTypeFile {
		resp := map[string]interface{}{
			"id":         entry.ID.String(),
			"word":       entry.Word,
			"status":     entry.Status,
			"entry_type": entry.EntryType,
			"created_at": entry.CreatedAt,
			"updated_at": entry.UpdatedAt,
			"hint":       "This is a file entry. Use the download_file tool with word '" + entry.Word + "' to get a download URL.",
		}
		if entry.ExpiresAt != nil {
			resp["expires_at"] = entry.ExpiresAt
		}
		return marshalResult(resp)
	}
	return marshalResult(entry)
}

func (h *Handlers) GetEntryByWord(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	word := req.GetString("word", "")
	includeExpired := req.GetBool("include_expired", false)

	h.logger.Debug("get_entry_by_word called", "word", word, "include_expired", includeExpired)

	entry, err := h.svc.GetEntryByWord(ctx, word, includeExpired)
	if err != nil {
		h.logger.Warn("get_entry_by_word failed", "word", word, "error", err)
		return mcp.NewToolResultError(err.Error()), nil
	}

	h.logger.Debug("get_entry_by_word success", "word", word, "id", entry.ID, "status", entry.Status)

	if entry.EntryType == domain.EntryTypeFile {
		resp := map[string]interface{}{
			"id":         entry.ID.String(),
			"word":       entry.Word,
			"status":     entry.Status,
			"entry_type": entry.EntryType,
			"created_at": entry.CreatedAt,
			"updated_at": entry.UpdatedAt,
			"hint":       "This is a file entry. Use the download_file tool with word '" + entry.Word + "' to get a download URL.",
		}
		if entry.ExpiresAt != nil {
			resp["expires_at"] = entry.ExpiresAt
		}
		return marshalResult(resp)
	}
	return marshalResult(entry)
}

func (h *Handlers) SearchEntries(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	pattern := req.GetString("pattern", "")
	status := req.GetString("status", "")
	limit := req.GetInt("limit", 20)
	offset := req.GetInt("offset", 0)

	h.logger.Debug("search_entries called", "pattern", pattern, "status", status, "limit", limit, "offset", offset)

	entries, total, err := h.svc.SearchEntries(ctx, pattern, status, limit, offset)
	if err != nil {
		h.logger.Warn("search_entries failed", "pattern", pattern, "error", err)
		return mcp.NewToolResultError(err.Error()), nil
	}

	h.logger.Debug("search_entries success", "pattern", pattern, "total", total, "returned", len(entries))

	resp := map[string]interface{}{
		"entries": entries,
		"total":   total,
		"limit":   limit,
		"offset":  offset,
	}
	return marshalResult(resp)
}

func (h *Handlers) RestoreEntry(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	id := req.GetString("id", "")

	var newTTLHours *int
	if args := req.GetArguments(); args != nil {
		if _, ok := args["new_ttl_hours"]; ok {
			t := req.GetInt("new_ttl_hours", 0)
			newTTLHours = &t
		}
	}

	h.logger.Debug("restore_entry called", "id", id, "new_ttl_hours", newTTLHours)

	result, err := h.svc.RestoreEntry(ctx, id, newTTLHours)
	if err != nil {
		h.logger.Warn("restore_entry failed", "id", id, "error", err)
		return mcp.NewToolResultError(err.Error()), nil
	}

	h.logger.Info("restore_entry success", "id", result.Entry.ID, "word", result.Entry.Word, "collision", result.CollisionResolved)

	resp := map[string]interface{}{
		"id":                 result.Entry.ID.String(),
		"word":               result.Entry.Word,
		"status":             result.Entry.Status,
		"collision_resolved": result.CollisionResolved,
	}
	if result.Entry.ExpiresAt != nil {
		resp["expires_at"] = result.Entry.ExpiresAt
	}
	if result.CollisionResolved {
		resp["original_word"] = result.OriginalWord
	}

	return marshalResult(resp)
}

func (h *Handlers) ListEntries(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	status := req.GetString("status", "")
	limit := req.GetInt("limit", 20)
	offset := req.GetInt("offset", 0)
	sortBy := req.GetString("sort_by", "")
	sortOrder := req.GetString("sort_order", "")

	h.logger.Debug("list_entries called", "status", status, "limit", limit, "offset", offset, "sort_by", sortBy, "sort_order", sortOrder)

	entries, total, err := h.svc.ListEntries(ctx, status, limit, offset, sortBy, sortOrder)
	if err != nil {
		h.logger.Warn("list_entries failed", "error", err)
		return mcp.NewToolResultError(err.Error()), nil
	}

	h.logger.Debug("list_entries success", "total", total, "returned", len(entries))

	resp := map[string]interface{}{
		"entries": entries,
		"total":   total,
		"limit":   limit,
		"offset":  offset,
	}
	return marshalResult(resp)
}

func (h *Handlers) DeleteEntry(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	id := req.GetString("id", "")

	h.logger.Debug("delete_entry called", "id", id)

	if err := h.svc.DeleteEntry(ctx, id); err != nil {
		h.logger.Warn("delete_entry failed", "id", id, "error", err)
		return mcp.NewToolResultError(err.Error()), nil
	}

	h.logger.Info("delete_entry success", "id", id)

	return marshalResult(map[string]interface{}{
		"deleted": true,
		"id":      id,
	})
}

func marshalResult(v interface{}) (*mcp.CallToolResult, error) {
	data, err := json.Marshal(v)
	if err != nil {
		return mcp.NewToolResultError("internal error: failed to marshal response"), nil
	}
	return mcp.NewToolResultText(string(data)), nil
}
