package callmebot

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/google/uuid"

	"osyn/internal/retry"
)

type Config struct {
	APIKey      string
	BaseURL     string
	HTTPTimeout time.Duration
}

type Client struct {
	apiKey   string
	endpoint string
	http     *http.Client
}

type HTTPError struct {
	StatusCode int
	Message    string
}

func (e *HTTPError) Error() string {
	return fmt.Sprintf("callmebot returned status %d: %s", e.StatusCode, e.Message)
}

func NewClient(cfg Config) (*Client, error) {
	if strings.TrimSpace(cfg.APIKey) == "" {
		return nil, fmt.Errorf("callmebot api key is required")
	}
	if cfg.BaseURL == "" {
		cfg.BaseURL = "https://api.callmebot.com/whatsapp.php"
	}
	parsed, err := url.Parse(cfg.BaseURL)
	if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return nil, fmt.Errorf("invalid callmebot base URL")
	}
	if cfg.HTTPTimeout <= 0 {
		cfg.HTTPTimeout = 20 * time.Second
	}
	return &Client{
		apiKey:   cfg.APIKey,
		endpoint: cfg.BaseURL,
		http:     &http.Client{Timeout: cfg.HTTPTimeout},
	}, nil
}

func (c *Client) Send(ctx context.Context, to, body string) (string, error) {
	endpoint, err := url.Parse(c.endpoint)
	if err != nil {
		return "", fmt.Errorf("build callmebot URL: %w", err)
	}
	query := endpoint.Query()
	query.Set("phone", to)
	query.Set("text", body)
	query.Set("apikey", c.apiKey)
	endpoint.RawQuery = query.Encode()

	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint.String(), nil)
	if err != nil {
		return "", fmt.Errorf("build callmebot request: %w", err)
	}
	request.Header.Set("Accept", "text/plain")
	request.Header.Set("User-Agent", "osyn/1.0")
	response, err := c.http.Do(request)
	if err != nil {
		return "", fmt.Errorf("send callmebot message: %w", err)
	}
	defer response.Body.Close()
	data, err := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	if err != nil {
		return "", fmt.Errorf("read callmebot response: %w", err)
	}
	message := strings.TrimSpace(string(data))
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return "", classifyHTTPError(response.StatusCode, message)
	}
	if strings.HasPrefix(strings.ToUpper(message), "ERROR") {
		return "", retry.Permanent(fmt.Errorf("callmebot rejected the message: %s", truncate(message, 512)))
	}
	if message == "" {
		return "", fmt.Errorf("callmebot returned an empty response")
	}
	return "callmebot-" + uuid.NewString(), nil
}

func classifyHTTPError(statusCode int, message string) error {
	httpErr := &HTTPError{StatusCode: statusCode, Message: truncate(message, 512)}
	if statusCode == http.StatusTooManyRequests || statusCode == http.StatusRequestTimeout || statusCode >= http.StatusInternalServerError {
		return httpErr
	}
	return retry.Permanent(httpErr)
}

func truncate(value string, limit int) string {
	if len(value) <= limit {
		return value
	}
	return value[:limit]
}
