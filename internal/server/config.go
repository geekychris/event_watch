package server

import (
	"flag"
	"os"
	"time"
)

type Config struct {
	Addr             string
	Store            string        // "memory" or "redis"
	RedisAddr        string
	RedisPassword    string
	RedisDB          int
	Auth             string        // "" or "bearer"
	AuthToken        string
	DefaultTTL       time.Duration
	ArchiveInterval  time.Duration
	GitHubSecret     string
}

func envDefault(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

// LoadConfig parses flags and environment. Env vars take precedence over
// defaults; flags take precedence over env.
func LoadConfig(args []string) (*Config, error) {
	c := &Config{}
	fs := flag.NewFlagSet("eventwatch", flag.ContinueOnError)
	fs.StringVar(&c.Addr, "addr", envDefault("EW_ADDR", ":8080"), "listen address")
	fs.StringVar(&c.Store, "store", envDefault("EW_STORE", "memory"), "memory|redis")
	fs.StringVar(&c.RedisAddr, "redis-addr", envDefault("EW_REDIS_ADDR", "localhost:6379"), "redis address")
	fs.StringVar(&c.RedisPassword, "redis-password", envDefault("EW_REDIS_PASSWORD", ""), "redis password")
	fs.IntVar(&c.RedisDB, "redis-db", 0, "redis database")
	fs.StringVar(&c.Auth, "auth", envDefault("EW_AUTH", ""), "auth mode (empty=disabled, bearer)")
	fs.StringVar(&c.AuthToken, "auth-token", envDefault("EW_AUTH_TOKEN", ""), "bearer token when --auth=bearer")
	fs.DurationVar(&c.DefaultTTL, "default-ttl", 168*time.Hour, "default topic TTL")
	fs.DurationVar(&c.ArchiveInterval, "archive-interval", 5*time.Minute, "TTL sweeper interval")
	fs.StringVar(&c.GitHubSecret, "github-secret", envDefault("EW_GITHUB_SECRET", ""), "GitHub webhook shared secret")
	if err := fs.Parse(args); err != nil {
		return nil, err
	}
	return c, nil
}
