package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Server     ServerConfig     `yaml:"server"`
	Database   DatabaseConfig   `yaml:"database"`
	Auth       AuthConfig       `yaml:"auth"`
	Expiration ExpirationConfig `yaml:"expiration"`
	Logging    LoggingConfig    `yaml:"logging"`
	Tools      ToolsConfig      `yaml:"tools"`
}

type ToolsConfig struct {
	StoreEntry     ToolDesc `yaml:"store_entry"`
	GetEntry       ToolDesc `yaml:"get_entry"`
	GetEntryByWord ToolDesc `yaml:"get_entry_by_word"`
	SearchEntries  ToolDesc `yaml:"search_entries"`
	RestoreEntry   ToolDesc `yaml:"restore_entry"`
	ListEntries    ToolDesc `yaml:"list_entries"`
	DeleteEntry    ToolDesc `yaml:"delete_entry"`
}

type ToolDesc struct {
	Description string            `yaml:"description"`
	Properties  map[string]string `yaml:"properties"`
}

var defaultToolsConfig = ToolsConfig{
	StoreEntry: ToolDesc{
		Description: "Store a payload under a memorable keyword. You (the LLM) generate the keyword — pick something short and memorable (a single English word, e.g., an animal name, a color, an object) or a short phrase. If the keyword is already taken, the server automatically appends a number suffix (e.g., 'rabbit' → 'rabbit2'). IMPORTANT: Always tell the user the RETURNED key from the response, not the one you submitted, because the server may have modified it to avoid conflicts.",
		Properties: map[string]string{
			"word":      "A memorable keyword or short phrase for this entry.",
			"payload":   "The content/prompt to store.",
			"ttl_hours": "Optional. Override default TTL in hours. 0 = never expires.",
		},
	},
	GetEntry: ToolDesc{
		Description: "Retrieve a stored entry by its unique ID (UUID).",
		Properties: map[string]string{
			"id": "The UUID of the entry to retrieve.",
		},
	},
	GetEntryByWord: ToolDesc{
		Description: "Retrieve a stored entry by its exact keyword. Returns the active entry matching this word.",
		Properties: map[string]string{
			"word":            "The exact keyword to look up.",
			"include_expired": "If true, also search expired entries. Default: false.",
		},
	},
	SearchEntries: ToolDesc{
		Description: "Search for entries whose keyword matches a pattern. Uses SQL LIKE syntax: '%' matches any sequence, '_' matches one character. Example: 'rab%' finds 'rabbit', 'rabbit2', 'rabbit3'.",
		Properties: map[string]string{
			"pattern": "LIKE pattern to match against keywords. Example: '%cat%' finds any word containing 'cat'.",
			"status":  "Filter by status: 'active', 'expired', or 'all'. Default: 'active'.",
			"limit":   "Maximum number of results. Default: 20, max: 100.",
			"offset":  "Pagination offset. Default: 0.",
		},
	},
	RestoreEntry: ToolDesc{
		Description: "Restore an expired entry back to active status. If the original word is already taken by another active entry, the server resolves the collision by appending a number suffix (same algorithm as store_entry). IMPORTANT: Always tell the user the RETURNED word, as it may differ from the original if a collision was resolved.",
		Properties: map[string]string{
			"id":            "The UUID of the expired entry to restore.",
			"new_ttl_hours": "Optional. Set a new TTL in hours from now. 0 = never expires. Omit to use server default.",
		},
	},
	ListEntries: ToolDesc{
		Description: "List stored entries with optional filtering by status and pagination.",
		Properties: map[string]string{
			"status":     "Filter by status: 'active', 'expired', or 'all'. Default: 'active'.",
			"limit":      "Maximum number of results. Default: 20, max: 100.",
			"offset":     "Pagination offset. Default: 0.",
			"sort_by":    "Sort field: 'created_at', 'updated_at', or 'word'. Default: 'created_at'.",
			"sort_order": "Sort direction: 'asc' or 'desc'. Default: 'desc'.",
		},
	},
	DeleteEntry: ToolDesc{
		Description: "Permanently delete a stored entry by its UUID. This action is irreversible.",
		Properties: map[string]string{
			"id": "The UUID of the entry to delete.",
		},
	},
}

type ServerConfig struct {
	Transport  string `yaml:"transport"`
	SSEPort    int    `yaml:"sse_port"`
	HTTPPort   int    `yaml:"http_port"`
	HealthPort int    `yaml:"health_port"`
}

type DatabaseConfig struct {
	Driver   string         `yaml:"driver"`
	SQLite   SQLiteConfig   `yaml:"sqlite"`
	Postgres PostgresConfig `yaml:"postgres"`
}

type SQLiteConfig struct {
	Path string `yaml:"path"`
}

type PostgresConfig struct {
	DSN string `yaml:"dsn"`
}

type AuthConfig struct {
	Enabled          bool                      `yaml:"enabled"`
	Tokens           []string                  `yaml:"tokens"`
	JWT              *JWTConfig                `yaml:"jwt"`
	OAuthMetadata    *OAuthMetadataConfig      `yaml:"oauth_metadata"`
	ResourceMetadata *ResourceMetadataConfig   `yaml:"resource_metadata"`
}

type JWTConfig struct {
	JWKSURL  string `yaml:"jwks_url"`
	Issuer   string `yaml:"issuer"`
	Audience string `yaml:"audience"`
}

type OAuthMetadataConfig struct {
	AuthorizationEndpoint string `yaml:"authorization_endpoint"`
	TokenEndpoint         string `yaml:"token_endpoint"`
}

// ResourceMetadataConfig configures the RFC 9728 Protected Resource Metadata
// endpoint at /.well-known/oauth-protected-resource.
type ResourceMetadataConfig struct {
	// Resource is the canonical URI of this MCP server (e.g. "https://watchword.example.com").
	Resource string `yaml:"resource"`
	// AuthorizationServers lists one or more authorization server issuer URIs.
	AuthorizationServers []string `yaml:"authorization_servers"`
	// BearerMethodsSupported indicates how tokens are sent. Defaults to ["header"].
	BearerMethodsSupported []string `yaml:"bearer_methods_supported"`
	// ScopesSupported lists the scopes this resource server understands (optional).
	ScopesSupported []string `yaml:"scopes_supported"`
}

type ExpirationConfig struct {
	Enabled       bool `yaml:"enabled"`
	IntervalHours int  `yaml:"interval_hours"`
	TTLHours      int  `yaml:"ttl_hours"`
}

type LoggingConfig struct {
	Level  string `yaml:"level"`
	Format string `yaml:"format"`
	File   string `yaml:"file"`
}

func Load(path string) (*Config, error) {
	cfg := &Config{
		Server:     ServerConfig{Transport: "stdio", SSEPort: 8080, HTTPPort: 8080, HealthPort: 8081},
		Database:   DatabaseConfig{Driver: "sqlite", SQLite: SQLiteConfig{Path: "./data/word-store.db"}},
		Auth:       AuthConfig{Enabled: true},
		Expiration: ExpirationConfig{Enabled: true, IntervalHours: 24, TTLHours: 168},
		Logging:    LoggingConfig{Level: "info", Format: "json"},
		Tools:      defaultToolsConfig,
	}

	if path != "" {
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("reading config file: %w", err)
		}
		if err := yaml.Unmarshal(data, cfg); err != nil {
			return nil, fmt.Errorf("parsing config file: %w", err)
		}
	}

	applyEnvOverrides(cfg)
	applyToolDefaults(cfg)

	if err := validate(cfg); err != nil {
		return nil, err
	}

	return cfg, nil
}

func applyEnvOverrides(cfg *Config) {
	if v := os.Getenv("WORDSTORE_SERVER_TRANSPORT"); v != "" {
		cfg.Server.Transport = v
	}
	if v := os.Getenv("WORDSTORE_SERVER_SSE_PORT"); v != "" {
		if p, err := strconv.Atoi(v); err == nil {
			cfg.Server.SSEPort = p
		}
	}
	if v := os.Getenv("WORDSTORE_SERVER_HTTP_PORT"); v != "" {
		if p, err := strconv.Atoi(v); err == nil {
			cfg.Server.HTTPPort = p
		}
	}
	if v := os.Getenv("WORDSTORE_SERVER_HEALTH_PORT"); v != "" {
		if p, err := strconv.Atoi(v); err == nil {
			cfg.Server.HealthPort = p
		}
	}
	if v := os.Getenv("WORDSTORE_DATABASE_DRIVER"); v != "" {
		cfg.Database.Driver = v
	}
	if v := os.Getenv("WORDSTORE_DATABASE_SQLITE_PATH"); v != "" {
		cfg.Database.SQLite.Path = v
	}
	if v := os.Getenv("WORDSTORE_DATABASE_POSTGRES_DSN"); v != "" {
		cfg.Database.Postgres.DSN = v
	}
	if v := os.Getenv("WORDSTORE_AUTH_ENABLED"); v != "" {
		cfg.Auth.Enabled = v == "true" || v == "1"
	}
	if v := os.Getenv("WORDSTORE_AUTH_TOKENS"); v != "" {
		cfg.Auth.Tokens = strings.Split(v, ",")
	}
	if v := os.Getenv("WORDSTORE_AUTH_JWT_JWKS_URL"); v != "" {
		if cfg.Auth.JWT == nil {
			cfg.Auth.JWT = &JWTConfig{}
		}
		cfg.Auth.JWT.JWKSURL = v
	}
	if v := os.Getenv("WORDSTORE_AUTH_JWT_ISSUER"); v != "" {
		if cfg.Auth.JWT == nil {
			cfg.Auth.JWT = &JWTConfig{}
		}
		cfg.Auth.JWT.Issuer = v
	}
	if v := os.Getenv("WORDSTORE_AUTH_JWT_AUDIENCE"); v != "" {
		if cfg.Auth.JWT == nil {
			cfg.Auth.JWT = &JWTConfig{}
		}
		cfg.Auth.JWT.Audience = v
	}
	if v := os.Getenv("WORDSTORE_AUTH_OAUTH_AUTHORIZATION_ENDPOINT"); v != "" {
		if cfg.Auth.OAuthMetadata == nil {
			cfg.Auth.OAuthMetadata = &OAuthMetadataConfig{}
		}
		cfg.Auth.OAuthMetadata.AuthorizationEndpoint = v
	}
	if v := os.Getenv("WORDSTORE_AUTH_OAUTH_TOKEN_ENDPOINT"); v != "" {
		if cfg.Auth.OAuthMetadata == nil {
			cfg.Auth.OAuthMetadata = &OAuthMetadataConfig{}
		}
		cfg.Auth.OAuthMetadata.TokenEndpoint = v
	}
	if v := os.Getenv("WORDSTORE_AUTH_RESOURCE"); v != "" {
		if cfg.Auth.ResourceMetadata == nil {
			cfg.Auth.ResourceMetadata = &ResourceMetadataConfig{}
		}
		cfg.Auth.ResourceMetadata.Resource = v
	}
	if v := os.Getenv("WORDSTORE_AUTH_AUTHORIZATION_SERVERS"); v != "" {
		if cfg.Auth.ResourceMetadata == nil {
			cfg.Auth.ResourceMetadata = &ResourceMetadataConfig{}
		}
		cfg.Auth.ResourceMetadata.AuthorizationServers = strings.Split(v, ",")
	}
	if v := os.Getenv("WORDSTORE_AUTH_BEARER_METHODS"); v != "" {
		if cfg.Auth.ResourceMetadata == nil {
			cfg.Auth.ResourceMetadata = &ResourceMetadataConfig{}
		}
		cfg.Auth.ResourceMetadata.BearerMethodsSupported = strings.Split(v, ",")
	}
	if v := os.Getenv("WORDSTORE_AUTH_SCOPES_SUPPORTED"); v != "" {
		if cfg.Auth.ResourceMetadata == nil {
			cfg.Auth.ResourceMetadata = &ResourceMetadataConfig{}
		}
		cfg.Auth.ResourceMetadata.ScopesSupported = strings.Split(v, ",")
	}
	if v := os.Getenv("WORDSTORE_EXPIRATION_ENABLED"); v != "" {
		cfg.Expiration.Enabled = v == "true" || v == "1"
	}
	if v := os.Getenv("WORDSTORE_EXPIRATION_INTERVAL_HOURS"); v != "" {
		if h, err := strconv.Atoi(v); err == nil {
			cfg.Expiration.IntervalHours = h
		}
	}
	if v := os.Getenv("WORDSTORE_EXPIRATION_TTL_HOURS"); v != "" {
		if h, err := strconv.Atoi(v); err == nil {
			cfg.Expiration.TTLHours = h
		}
	}
	if v := os.Getenv("WORDSTORE_LOGGING_LEVEL"); v != "" {
		cfg.Logging.Level = v
	}
	if v := os.Getenv("WORDSTORE_LOGGING_FORMAT"); v != "" {
		cfg.Logging.Format = v
	}
	if v := os.Getenv("WORDSTORE_LOGGING_FILE"); v != "" {
		cfg.Logging.File = v
	}
}

func mergeToolDesc(dst *ToolDesc, def ToolDesc) {
	if dst.Description == "" {
		dst.Description = def.Description
	}
	if dst.Properties == nil {
		dst.Properties = def.Properties
	} else {
		for k, v := range def.Properties {
			if _, ok := dst.Properties[k]; !ok {
				dst.Properties[k] = v
			}
		}
	}
}

func applyToolDefaults(cfg *Config) {
	d := defaultToolsConfig
	mergeToolDesc(&cfg.Tools.StoreEntry, d.StoreEntry)
	mergeToolDesc(&cfg.Tools.GetEntry, d.GetEntry)
	mergeToolDesc(&cfg.Tools.GetEntryByWord, d.GetEntryByWord)
	mergeToolDesc(&cfg.Tools.SearchEntries, d.SearchEntries)
	mergeToolDesc(&cfg.Tools.RestoreEntry, d.RestoreEntry)
	mergeToolDesc(&cfg.Tools.ListEntries, d.ListEntries)
	mergeToolDesc(&cfg.Tools.DeleteEntry, d.DeleteEntry)
}

func validate(cfg *Config) error {
	switch cfg.Server.Transport {
	case "stdio", "sse", "streamable-http", "http":
	default:
		return fmt.Errorf("invalid server.transport: %q (must be stdio, sse, streamable-http, or http)", cfg.Server.Transport)
	}
	switch cfg.Database.Driver {
	case "sqlite", "postgres":
	default:
		return fmt.Errorf("invalid database.driver: %q (must be sqlite or postgres)", cfg.Database.Driver)
	}
	if cfg.Database.Driver == "sqlite" && cfg.Database.SQLite.Path == "" {
		return fmt.Errorf("database.sqlite.path is required when driver is sqlite")
	}
	if cfg.Database.Driver == "postgres" && cfg.Database.Postgres.DSN == "" {
		return fmt.Errorf("database.postgres.dsn is required when driver is postgres")
	}
	if cfg.Auth.JWT != nil && cfg.Auth.JWT.JWKSURL == "" {
		return fmt.Errorf("auth.jwt.jwks_url is required when jwt is configured")
	}
	if rm := cfg.Auth.ResourceMetadata; rm != nil {
		if rm.Resource == "" {
			return fmt.Errorf("auth.resource_metadata.resource is required when resource_metadata is configured")
		}
		if len(rm.AuthorizationServers) == 0 {
			return fmt.Errorf("auth.resource_metadata.authorization_servers must contain at least one entry")
		}
	}
	if cfg.Expiration.IntervalHours < 1 {
		return fmt.Errorf("expiration.interval_hours must be >= 1")
	}
	if cfg.Expiration.TTLHours < 0 {
		return fmt.Errorf("expiration.ttl_hours must be >= 0")
	}
	return nil
}
