package clio

import (
	"fmt"
	"os"
	"strconv"
	"time"
)

// Config holds all configuration for the clio agent.
type Config struct {
	Repo           string
	Release        string
	Target         string // optional: narrow to specific app.kubernetes.io/name
	GitHubToken    string
	Namespace      string
	Cooldown       time.Duration
	MaxConcurrency int
	TailLines      int64
	MaxPRsPerHour  int
	BatchWindow    time.Duration
	DryRun         bool
	Port           int
	MaxAgentTurns  int    // max Claude Code agent turns per session
	MaxAgentBudget string // max dollar cost per agent session
}

// LoadConfig reads configuration from environment variables.
func LoadConfig() (Config, error) {
	c := Config{
		Repo:           os.Getenv("CLIO_REPO"),
		Release:        os.Getenv("CLIO_RELEASE"),
		Target:         os.Getenv("CLIO_TARGET"),
		GitHubToken:    os.Getenv("GITHUB_TOKEN"),
		Namespace:      os.Getenv("CLIO_NAMESPACE"),
		MaxAgentBudget: envOrDefault("CLIO_MAX_AGENT_BUDGET", "1.00"),
	}

	if c.Repo == "" {
		return c, fmt.Errorf("CLIO_REPO is required")
	}
	if c.Release == "" {
		return c, fmt.Errorf("CLIO_RELEASE is required")
	}
	if os.Getenv("ANTHROPIC_API_KEY") == "" {
		return c, fmt.Errorf("ANTHROPIC_API_KEY is required")
	}
	if c.GitHubToken == "" {
		return c, fmt.Errorf("GITHUB_TOKEN is required")
	}

	if c.Namespace == "" {
		ns, err := os.ReadFile("/var/run/secrets/kubernetes.io/serviceaccount/namespace")
		if err != nil {
			return c, fmt.Errorf("CLIO_NAMESPACE is required (not running in-cluster)")
		}
		c.Namespace = string(ns)
	}

	var err error
	c.Cooldown, err = parseDuration("CLIO_COOLDOWN", "1h")
	if err != nil {
		return c, err
	}
	c.BatchWindow, err = parseDuration("CLIO_BATCH_WINDOW", "5s")
	if err != nil {
		return c, err
	}
	c.MaxConcurrency, err = parseInt("CLIO_MAX_CONCURRENCY", 3)
	if err != nil {
		return c, err
	}
	c.TailLines, err = parseInt64("CLIO_TAIL_LINES", 100)
	if err != nil {
		return c, err
	}
	c.MaxPRsPerHour, err = parseInt("CLIO_MAX_PRS_PER_HOUR", 5)
	if err != nil {
		return c, err
	}
	c.Port, err = parseInt("CLIO_PORT", 8080)
	if err != nil {
		return c, err
	}
	c.DryRun, err = parseBool("CLIO_DRY_RUN", false)
	if err != nil {
		return c, err
	}
	c.MaxAgentTurns, err = parseInt("CLIO_MAX_AGENT_TURNS", 25)
	if err != nil {
		return c, err
	}

	return c, nil
}

func envOrDefault(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func parseDuration(key, def string) (time.Duration, error) {
	return time.ParseDuration(envOrDefault(key, def))
}

func parseInt(key string, def int) (int, error) {
	v := os.Getenv(key)
	if v == "" {
		return def, nil
	}
	return strconv.Atoi(v)
}

func parseInt64(key string, def int64) (int64, error) {
	v := os.Getenv(key)
	if v == "" {
		return def, nil
	}
	return strconv.ParseInt(v, 10, 64)
}

func parseBool(key string, def bool) (bool, error) {
	v := os.Getenv(key)
	if v == "" {
		return def, nil
	}
	return strconv.ParseBool(v)
}
