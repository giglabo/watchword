package mcp

import (
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"

	"github.com/watchword/watchword/internal/repository"
	"github.com/watchword/watchword/internal/service"
)

func newTestHandlers(t *testing.T) *Handlers {
	t.Helper()
	repo, err := repository.NewSQLiteRepo(":memory:")
	if err != nil {
		t.Fatalf("NewSQLiteRepo: %v", err)
	}
	t.Cleanup(func() { repo.Close() })
	if err := repo.Migrate(context.Background()); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	svc := service.NewEntryService(repo, 168, logger)
	return &Handlers{svc: svc, logger: logger}
}

func callTool(t *testing.T, h *Handlers, name string, args map[string]interface{}) map[string]interface{} {
	t.Helper()
	req := mcp.CallToolRequest{}
	req.Params.Name = name
	req.Params.Arguments = args

	var result *mcp.CallToolResult
	var err error

	ctx := context.Background()
	switch name {
	case "store_entry":
		result, err = h.StoreEntry(ctx, req)
	case "get_entry":
		result, err = h.GetEntry(ctx, req)
	case "get_entry_by_word":
		result, err = h.GetEntryByWord(ctx, req)
	case "search_entries":
		result, err = h.SearchEntries(ctx, req)
	case "list_entries":
		result, err = h.ListEntries(ctx, req)
	case "restore_entry":
		result, err = h.RestoreEntry(ctx, req)
	case "delete_entry":
		result, err = h.DeleteEntry(ctx, req)
	default:
		t.Fatalf("unknown tool: %s", name)
	}

	if err != nil {
		t.Fatalf("tool %s returned error: %v", name, err)
	}

	// Extract text from result
	text := result.Content[0].(mcp.TextContent).Text
	var out map[string]interface{}
	if err := json.Unmarshal([]byte(text), &out); err != nil {
		// Might be an error result
		t.Logf("tool %s raw output: %s", name, text)
		return map[string]interface{}{"_error": text}
	}
	return out
}

func TestMCP_StoreEntry(t *testing.T) {
	h := newTestHandlers(t)

	out := callTool(t, h, "store_entry", map[string]interface{}{
		"word":    "rabbit",
		"payload": "test payload",
	})
	if out["word"] != "rabbit" {
		t.Errorf("expected word=rabbit, got %v", out["word"])
	}
	if out["status"] != "active" {
		t.Errorf("expected status=active, got %v", out["status"])
	}
	if out["collision_resolved"] != false {
		t.Errorf("expected collision_resolved=false, got %v", out["collision_resolved"])
	}
	if out["id"] == nil || out["id"] == "" {
		t.Error("expected non-empty id")
	}
}

func TestMCP_StoreEntry_Collision(t *testing.T) {
	h := newTestHandlers(t)

	callTool(t, h, "store_entry", map[string]interface{}{
		"word": "cat", "payload": "first",
	})
	out := callTool(t, h, "store_entry", map[string]interface{}{
		"word": "cat", "payload": "second",
	})
	if out["word"] != "cat2" {
		t.Errorf("expected word=cat2, got %v", out["word"])
	}
	if out["collision_resolved"] != true {
		t.Errorf("expected collision_resolved=true")
	}
	if out["original_word"] != "cat" {
		t.Errorf("expected original_word=cat, got %v", out["original_word"])
	}
}

func TestMCP_GetEntry(t *testing.T) {
	h := newTestHandlers(t)

	stored := callTool(t, h, "store_entry", map[string]interface{}{
		"word": "fox", "payload": "quick brown fox",
	})

	out := callTool(t, h, "get_entry", map[string]interface{}{
		"id": stored["id"],
	})
	if out["word"] != "fox" {
		t.Errorf("expected word=fox, got %v", out["word"])
	}
	if out["payload"] != "quick brown fox" {
		t.Errorf("expected payload='quick brown fox', got %v", out["payload"])
	}
}

func TestMCP_GetEntry_NotFound(t *testing.T) {
	h := newTestHandlers(t)
	out := callTool(t, h, "get_entry", map[string]interface{}{
		"id": "00000000-0000-0000-0000-000000000000",
	})
	if _, ok := out["_error"]; !ok {
		t.Error("expected error for non-existent entry")
	}
}

func TestMCP_GetEntryByWord(t *testing.T) {
	h := newTestHandlers(t)

	callTool(t, h, "store_entry", map[string]interface{}{
		"word": "owl", "payload": "hoot",
	})

	out := callTool(t, h, "get_entry_by_word", map[string]interface{}{
		"word": "owl",
	})
	if out["word"] != "owl" {
		t.Errorf("expected word=owl, got %v", out["word"])
	}
	if out["payload"] != "hoot" {
		t.Errorf("expected payload=hoot, got %v", out["payload"])
	}
}

func TestMCP_SearchEntries(t *testing.T) {
	h := newTestHandlers(t)

	callTool(t, h, "store_entry", map[string]interface{}{"word": "rabbit", "payload": "p1"})
	callTool(t, h, "store_entry", map[string]interface{}{"word": "raccoon", "payload": "p2"})
	callTool(t, h, "store_entry", map[string]interface{}{"word": "dog", "payload": "p3"})

	out := callTool(t, h, "search_entries", map[string]interface{}{
		"pattern": "ra%",
	})
	total, ok := out["total"].(float64)
	if !ok || total != 2 {
		t.Errorf("expected total=2, got %v", out["total"])
	}
}

func TestMCP_ListEntries(t *testing.T) {
	h := newTestHandlers(t)

	callTool(t, h, "store_entry", map[string]interface{}{"word": "alpha", "payload": "p1"})
	callTool(t, h, "store_entry", map[string]interface{}{"word": "beta", "payload": "p2"})
	callTool(t, h, "store_entry", map[string]interface{}{"word": "gamma", "payload": "p3"})

	out := callTool(t, h, "list_entries", map[string]interface{}{
		"status":     "active",
		"sort_by":    "word",
		"sort_order": "asc",
		"limit":      float64(2),
	})
	total, ok := out["total"].(float64)
	if !ok || total != 3 {
		t.Errorf("expected total=3, got %v", out["total"])
	}
	entries := out["entries"].([]interface{})
	if len(entries) != 2 {
		t.Errorf("expected 2 entries (limit), got %d", len(entries))
	}
	first := entries[0].(map[string]interface{})
	if first["word"] != "alpha" {
		t.Errorf("expected first=alpha, got %v", first["word"])
	}
}

func TestMCP_DeleteEntry(t *testing.T) {
	h := newTestHandlers(t)

	stored := callTool(t, h, "store_entry", map[string]interface{}{
		"word": "temp", "payload": "delete me",
	})

	out := callTool(t, h, "delete_entry", map[string]interface{}{
		"id": stored["id"],
	})
	if out["deleted"] != true {
		t.Errorf("expected deleted=true, got %v", out["deleted"])
	}

	// Verify gone
	out = callTool(t, h, "get_entry", map[string]interface{}{
		"id": stored["id"],
	})
	if _, ok := out["_error"]; !ok {
		t.Error("expected error after delete")
	}
}

func TestMCP_RestoreEntry_AlreadyActive(t *testing.T) {
	h := newTestHandlers(t)

	stored := callTool(t, h, "store_entry", map[string]interface{}{
		"word": "alive", "payload": "still active",
	})

	out := callTool(t, h, "restore_entry", map[string]interface{}{
		"id": stored["id"],
	})
	if _, ok := out["_error"]; !ok {
		t.Error("expected error restoring active entry")
	}
}

func TestMCP_StoreEntry_InvalidWord(t *testing.T) {
	h := newTestHandlers(t)
	out := callTool(t, h, "store_entry", map[string]interface{}{
		"word": "has\ttab", "payload": "test",
	})
	if _, ok := out["_error"]; !ok {
		t.Error("expected error for control characters in word")
	}
}

func TestMCP_StoreEntry_Russian(t *testing.T) {
	h := newTestHandlers(t)
	out := callTool(t, h, "store_entry", map[string]interface{}{
		"word": "кролик", "payload": "тест",
	})
	if out["word"] != "кролик" {
		t.Errorf("expected word='кролик', got %v", out["word"])
	}
}

func TestMCP_StoreEntry_SentenceWord(t *testing.T) {
	h := newTestHandlers(t)
	out := callTool(t, h, "store_entry", map[string]interface{}{
		"word": "my favorite prompt", "payload": "sentence key works",
	})
	if out["word"] != "my favorite prompt" {
		t.Errorf("expected word='my favorite prompt', got %v", out["word"])
	}
}

func TestMCP_StoreEntry_EmptyPayload(t *testing.T) {
	h := newTestHandlers(t)
	out := callTool(t, h, "store_entry", map[string]interface{}{
		"word": "test", "payload": "",
	})
	if _, ok := out["_error"]; !ok {
		t.Error("expected error for empty payload")
	}
}

func TestMCP_StoreEntry_ZeroTTL(t *testing.T) {
	h := newTestHandlers(t)
	out := callTool(t, h, "store_entry", map[string]interface{}{
		"word": "permanent", "payload": "never expires", "ttl_hours": float64(0),
	})
	if out["word"] != "permanent" {
		t.Errorf("expected word=permanent, got %v", out["word"])
	}
	if out["expires_at"] != nil {
		t.Errorf("expected no expires_at for TTL=0, got %v", out["expires_at"])
	}
}

func TestMCP_MultipleCollisions(t *testing.T) {
	h := newTestHandlers(t)

	callTool(t, h, "store_entry", map[string]interface{}{"word": "bird", "payload": "p1"})
	callTool(t, h, "store_entry", map[string]interface{}{"word": "bird", "payload": "p2"}) // bird2
	callTool(t, h, "store_entry", map[string]interface{}{"word": "bird", "payload": "p3"}) // bird3

	out := callTool(t, h, "store_entry", map[string]interface{}{"word": "bird", "payload": "p4"})
	if out["word"] != "bird4" {
		t.Errorf("expected word=bird4, got %v", out["word"])
	}
}
