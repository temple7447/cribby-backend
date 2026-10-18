// Package config loads settings from the environment (and an optional .env
// file), mirroring the fields and defaults of the Python app/core/config.py.
package config

import (
	"bufio"
	"encoding/json"
	"os"
	"strconv"
	"strings"
)

type Settings struct {
	ProjectName string
	APIV1Str    string

	// Local dev defaults to SQLite (zero setup). Point DATABASE_URL at Postgres
	// for production. The Python-style driver suffixes ("+asyncpg",
	// "+aiosqlite") are accepted and ignored so the same value works unchanged.
	DatabaseURL string

	SecretKey                  string
	AccessTokenExpireMinutes   int
	BackendCORSOrigins         []string
	ResendAPIKey               string
	EmailFrom                  string
	EmailVerifyExpireMinutes   int
	EmailVerifyCooldownSeconds int
	PasswordResetExpireMinutes int
	PasswordResetCooldownSecs  int

	CloudinaryCloudName string
	CloudinaryAPIKey    string
	CloudinaryAPISecret string

	Port string
}

// Load reads .env (without overriding real environment variables, matching
// pydantic-settings) and then builds Settings from the environment.
func Load() *Settings {
	loadDotEnv(".env")
	return &Settings{
		ProjectName: str("PROJECT_NAME", "Cribby API"),
		APIV1Str:    str("API_V1_STR", "/api/v1"),
		DatabaseURL: str("DATABASE_URL", "sqlite+aiosqlite:///./campus.db"),

		SecretKey:                str("SECRET_KEY", "change-me-in-production"),
		AccessTokenExpireMinutes: integer("ACCESS_TOKEN_EXPIRE_MINUTES", 60*24*7), // 1 week
		BackendCORSOrigins: list("BACKEND_CORS_ORIGINS", []string{
			"http://localhost:8081",
			"http://localhost:8099",
			"http://localhost:19006",
			"http://localhost:5180",
			"https://admin.cribby.space",
		}),

		ResendAPIKey:               str("RESEND_API_KEY", ""),
		EmailFrom:                  str("EMAIL_FROM", "Cribby <onboarding@resend.dev>"),
		EmailVerifyExpireMinutes:   integer("EMAIL_VERIFICATION_TOKEN_EXPIRE_MINUTES", 30),
		EmailVerifyCooldownSeconds: integer("EMAIL_VERIFICATION_RESEND_COOLDOWN_SECONDS", 60),
		PasswordResetExpireMinutes: integer("PASSWORD_RESET_TOKEN_EXPIRE_MINUTES", 30),
		PasswordResetCooldownSecs:  integer("PASSWORD_RESET_RESEND_COOLDOWN_SECONDS", 60),

		CloudinaryCloudName: str("CLOUDINARY_CLOUD_NAME", ""),
		CloudinaryAPIKey:    str("CLOUDINARY_API_KEY", ""),
		CloudinaryAPISecret: str("CLOUDINARY_API_SECRET", ""),

		Port: str("PORT", "8000"),
	}
}

func str(key, def string) string {
	if v, ok := os.LookupEnv(key); ok {
		return v
	}
	return def
}

func integer(key string, def int) int {
	if v, ok := os.LookupEnv(key); ok {
		if n, err := strconv.Atoi(strings.TrimSpace(v)); err == nil {
			return n
		}
	}
	return def
}

// list accepts either a JSON array (pydantic-settings style) or a
// comma-separated string.
func list(key string, def []string) []string {
	v, ok := os.LookupEnv(key)
	if !ok || strings.TrimSpace(v) == "" {
		return def
	}
	v = strings.TrimSpace(v)
	if strings.HasPrefix(v, "[") {
		var out []string
		if err := json.Unmarshal([]byte(v), &out); err == nil {
			return out
		}
	}
	var out []string
	for _, p := range strings.Split(v, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

func loadDotEnv(path string) {
	f, err := os.Open(path)
	if err != nil {
		return
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		k, v = strings.TrimSpace(k), strings.TrimSpace(v)
		if len(v) >= 2 && (v[0] == '"' && v[len(v)-1] == '"' || v[0] == '\'' && v[len(v)-1] == '\'') {
			v = v[1 : len(v)-1]
		}
		if _, exists := os.LookupEnv(k); !exists {
			os.Setenv(k, v)
		}
	}
}
