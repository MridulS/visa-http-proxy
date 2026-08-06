package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"strconv"
	"time"
)

// Config holds the runtime configuration, sourced from VISA_JUPYTER_PROXY_* env
// vars (same names/defaults as the Node implementation).
type Config struct {
	ServerHost string
	ServerPort int
	APIBase    string // http://{API_HOST}:{API_PORT}/api
	CacheTTL   time.Duration
	StorageDir string
}

func envStr(key, def string) string {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		return v
	}
	return def
}

func envInt(key string, def int) int {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return def
}

// LoadConfig reads configuration from the environment.
func LoadConfig() Config {
	apiHost := envStr("VISA_JUPYTER_PROXY_API_HOST", "localhost")
	apiPort := envInt("VISA_JUPYTER_PROXY_API_PORT", 8086)
	return Config{
		ServerHost: envStr("VISA_JUPYTER_PROXY_SERVER_HOST", "0.0.0.0"),
		ServerPort: envInt("VISA_JUPYTER_PROXY_SERVER_PORT", 8088),
		APIBase:    fmt.Sprintf("http://%s:%d/api", apiHost, apiPort),
		CacheTTL:   time.Duration(envInt("VISA_JUPYTER_PROXY_CACHE_REFRESH_TIME_S", 60)) * time.Second,
		StorageDir: envStr("VISA_JUPYTER_PROXY_STORAGE_DIR", "data"),
	}
}

// RewriteRule is a single ordered path-rewrite rule.
type RewriteRule struct {
	Pattern *regexp.Regexp
	Repl    string
}

// RewriteRules preserves JSON declaration order (a Go map would not). Rules are
// applied in order; the first matching rule wins.
type RewriteRules []RewriteRule

// UnmarshalJSON walks the pathRewrite object with a streaming decoder so the
// keys are read in source order (encoding/json yields object keys in order).
func (r *RewriteRules) UnmarshalJSON(b []byte) error {
	dec := json.NewDecoder(bytes.NewReader(b))
	tok, err := dec.Token()
	if err != nil {
		return err
	}
	if d, ok := tok.(json.Delim); !ok || d != '{' {
		return fmt.Errorf("pathRewrite: expected a JSON object")
	}
	for dec.More() {
		keyTok, err := dec.Token()
		if err != nil {
			return err
		}
		key, ok := keyTok.(string)
		if !ok {
			return fmt.Errorf("pathRewrite: expected string key")
		}
		var repl string
		if err := dec.Decode(&repl); err != nil {
			return err
		}
		re, err := regexp.Compile(key)
		if err != nil {
			return fmt.Errorf("pathRewrite: invalid regex %q: %w", key, err)
		}
		*r = append(*r, RewriteRule{Pattern: re, Repl: repl})
	}
	_, err = dec.Token() // consume closing '}'
	return err
}

// Apply returns path with the first matching rule applied. Only the first regex
// match is replaced (matching JS String.replace(regexp, repl) semantics; the
// shipped patterns are ^-anchored so there is at most one match anyway).
func (r RewriteRules) Apply(path string) string {
	for _, rule := range r {
		loc := rule.Pattern.FindStringSubmatchIndex(path)
		if loc == nil {
			continue
		}
		repl := rule.Pattern.ExpandString(nil, rule.Repl, path, loc)
		return path[:loc[0]] + string(repl) + path[loc[1]:]
	}
	return path
}

// ProxyConf is one handler entry from proxy.conf.json.
type ProxyConf struct {
	Match       string       `json:"match"`
	Type        string       `json:"type"` // "jupyter" | "service"
	Name        string       `json:"name"`
	RemotePort  int          `json:"remotePort"`
	WS          bool         `json:"ws"`
	PathRewrite RewriteRules `json:"pathRewrite"`
	re          *regexp.Regexp
}

// LoadHandlers reads and compiles proxy.conf.json (cwd-relative).
func LoadHandlers(path string) ([]ProxyConf, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var confs []ProxyConf
	if err := json.Unmarshal(data, &confs); err != nil {
		return nil, err
	}
	for i := range confs {
		re, err := regexp.Compile(confs[i].Match)
		if err != nil {
			return nil, fmt.Errorf("handler %q: invalid match regex %q: %w", confs[i].Name, confs[i].Match, err)
		}
		if re.NumSubexp() < 1 {
			return nil, fmt.Errorf("handler %q: match %q must contain a capture group for the instance id", confs[i].Name, confs[i].Match)
		}
		confs[i].re = re
	}
	return confs, nil
}
