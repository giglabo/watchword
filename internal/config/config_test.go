package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoad_Defaults(t *testing.T) {
	cfg, err := Load("")
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}
	if cfg.Server.Transport != "stdio" {
		t.Errorf("expected transport=stdio, got %s", cfg.Server.Transport)
	}
	if cfg.Database.Driver != "sqlite" {
		t.Errorf("expected driver=sqlite, got %s", cfg.Database.Driver)
	}
	if cfg.Expiration.TTLHours != 168 {
		t.Errorf("expected ttl=168, got %d", cfg.Expiration.TTLHours)
	}
}

func TestLoad_YAMLFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	os.WriteFile(path, []byte(`
server:
  transport: "sse"
  sse_port: 9090
database:
  driver: "sqlite"
  sqlite:
    path: "./test.db"
`), 0644)

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}
	if cfg.Server.Transport != "sse" {
		t.Errorf("expected transport=sse, got %s", cfg.Server.Transport)
	}
	if cfg.Server.SSEPort != 9090 {
		t.Errorf("expected port=9090, got %d", cfg.Server.SSEPort)
	}
}

func TestLoad_EnvOverrides(t *testing.T) {
	t.Setenv("WORDSTORE_DATABASE_DRIVER", "sqlite")
	t.Setenv("WORDSTORE_DATABASE_SQLITE_PATH", "/tmp/test.db")
	t.Setenv("WORDSTORE_EXPIRATION_TTL_HOURS", "48")

	cfg, err := Load("")
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}
	if cfg.Database.SQLite.Path != "/tmp/test.db" {
		t.Errorf("expected path=/tmp/test.db, got %s", cfg.Database.SQLite.Path)
	}
	if cfg.Expiration.TTLHours != 48 {
		t.Errorf("expected ttl=48, got %d", cfg.Expiration.TTLHours)
	}
}

func TestLoad_InvalidTransport(t *testing.T) {
	t.Setenv("WORDSTORE_SERVER_TRANSPORT", "grpc")
	_, err := Load("")
	if err == nil {
		t.Error("expected error for invalid transport")
	}
}

func TestLoad_ResourceMetadata_EnvOverrides(t *testing.T) {
	t.Setenv("WORDSTORE_AUTH_RESOURCE", "https://watchword.example.com")
	t.Setenv("WORDSTORE_AUTH_AUTHORIZATION_SERVERS", "https://auth1.example.com,https://auth2.example.com")

	cfg, err := Load("")
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}
	if cfg.Auth.ResourceMetadata == nil {
		t.Fatal("expected resource_metadata to be set")
	}
	if cfg.Auth.ResourceMetadata.Resource != "https://watchword.example.com" {
		t.Errorf("expected resource=https://watchword.example.com, got %s", cfg.Auth.ResourceMetadata.Resource)
	}
	if len(cfg.Auth.ResourceMetadata.AuthorizationServers) != 2 {
		t.Errorf("expected 2 authorization_servers, got %d", len(cfg.Auth.ResourceMetadata.AuthorizationServers))
	}
}

func TestLoad_ResourceMetadata_MissingResource(t *testing.T) {
	t.Setenv("WORDSTORE_AUTH_AUTHORIZATION_SERVERS", "https://auth.example.com")

	_, err := Load("")
	if err == nil {
		t.Error("expected error when resource_metadata.resource is missing")
	}
}

func TestLoad_ResourceMetadata_MissingServers(t *testing.T) {
	t.Setenv("WORDSTORE_AUTH_RESOURCE", "https://watchword.example.com")

	_, err := Load("")
	if err == nil {
		t.Error("expected error when resource_metadata.authorization_servers is empty")
	}
}
