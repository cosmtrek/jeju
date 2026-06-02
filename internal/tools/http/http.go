package http

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	nethttp "net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/cosmtrek/jeju/internal/tools"
)

type Tool struct {
	name    string
	spec    tools.Spec
	method  string
	rawURL  string
	query   map[string]string
	headers map[string]string
	body    any
	timeout int
}

type Config struct {
	Name       string
	Spec       tools.Spec
	Method     string
	URL        string
	Query      map[string]string
	Headers    map[string]string
	Body       any
	TimeoutSec int
}

func New(cfg Config) *Tool {
	cfg.Spec.Name = cfg.Name
	return &Tool{
		name:    cfg.Name,
		spec:    cfg.Spec,
		method:  strings.ToUpper(cfg.Method),
		rawURL:  cfg.URL,
		query:   cfg.Query,
		headers: cfg.Headers,
		body:    cfg.Body,
		timeout: cfg.TimeoutSec,
	}
}

func (t *Tool) Name() string {
	return t.name
}

func (t *Tool) Spec() tools.Spec {
	return t.spec
}

func (t *Tool) Run(ctx context.Context, input json.RawMessage) (tools.Result, error) {
	values := map[string]any{}
	if len(input) > 0 {
		if err := json.Unmarshal(input, &values); err != nil {
			return tools.Result{}, err
		}
	}
	u, err := url.Parse(t.rawURL)
	if err != nil {
		return tools.Result{}, err
	}
	q := u.Query()
	for key, tmpl := range t.query {
		q.Set(key, renderTemplate(tmpl, values))
	}
	u.RawQuery = q.Encode()

	var body io.Reader
	if t.body != nil {
		data, err := json.Marshal(renderAny(t.body, values))
		if err != nil {
			return tools.Result{}, err
		}
		body = bytes.NewReader(data)
	}
	timeout := 30 * time.Second
	if t.timeout > 0 {
		timeout = time.Duration(t.timeout) * time.Second
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	req, err := nethttp.NewRequestWithContext(ctx, t.method, u.String(), body)
	if err != nil {
		return tools.Result{}, err
	}
	for key, value := range t.headers {
		req.Header.Set(key, renderTemplate(os.ExpandEnv(value), values))
	}
	if t.body != nil && req.Header.Get("Content-Type") == "" {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := nethttp.DefaultClient.Do(req)
	if err != nil {
		return tools.Result{}, err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return tools.Result{}, err
	}
	out, err := json.Marshal(map[string]any{
		"status": resp.StatusCode,
		"body":   string(data),
	})
	if err != nil {
		return tools.Result{}, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return tools.Result{Output: string(out)}, fmt.Errorf("http tool %s returned status %d", t.name, resp.StatusCode)
	}
	return tools.Result{Output: string(out)}, nil
}

func renderAny(value any, vars map[string]any) any {
	switch typed := value.(type) {
	case string:
		return renderTemplate(typed, vars)
	case map[string]any:
		out := map[string]any{}
		for key, value := range typed {
			out[key] = renderAny(value, vars)
		}
		return out
	case []any:
		out := make([]any, 0, len(typed))
		for _, value := range typed {
			out = append(out, renderAny(value, vars))
		}
		return out
	default:
		return value
	}
}

func renderTemplate(tmpl string, vars map[string]any) string {
	result := tmpl
	for key, value := range vars {
		result = strings.ReplaceAll(result, "{{"+key+"}}", fmt.Sprint(value))
	}
	return result
}
