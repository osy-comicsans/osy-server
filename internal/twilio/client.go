package twilio

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"osyn/internal/retry"
)

type Config struct {
	AccountSID          string
	AuthToken           string
	FromNumber          string
	MessagingServiceSID string
	BaseURL             string
	HTTPTimeout         time.Duration
}

type Client struct {
	accountSID          string
	authToken           string
	fromNumber          string
	messagingServiceSID string
	endpoint            string
	httpClient          *http.Client
}

type response struct {
	SID     string `json:"sid"`
	Status  string `json:"status"`
	Message string `json:"message"`
	Code    int    `json:"code"`
}

type HTTPError struct {
	StatusCode int
	Message    string
}

func (e *HTTPError) Error() string {
	return fmt.Sprintf("twilio returned status %d: %s", e.StatusCode, e.Message)
}

func NewClient(cfg Config) (*Client, error) {
	if strings.TrimSpace(cfg.AccountSID) == "" {
		return nil, fmt.Errorf("twilio account sid is required")
	}
	if strings.TrimSpace(cfg.AuthToken) == "" {
		return nil, fmt.Errorf("twilio auth token is required")
	}
	if strings.TrimSpace(cfg.FromNumber) == "" && strings.TrimSpace(cfg.MessagingServiceSID) == "" {
		return nil, fmt.Errorf("twilio from number or messaging service sid is required")
	}
	if cfg.BaseURL == "" {
		cfg.BaseURL = "https://api.twilio.com"
	}
	if cfg.HTTPTimeout <= 0 {
		cfg.HTTPTimeout = 15 * time.Second
	}
	endpoint := strings.TrimRight(cfg.BaseURL, "/") + "/2010-04-01/Accounts/" + url.PathEscape(cfg.AccountSID) + "/Messages.json"
	return &Client{
		accountSID:          cfg.AccountSID,
		authToken:           cfg.AuthToken,
		fromNumber:          cfg.FromNumber,
		messagingServiceSID: cfg.MessagingServiceSID,
		endpoint:            endpoint,
		httpClient:          &http.Client{Timeout: cfg.HTTPTimeout},
	}, nil
}

func (c *Client) Send(ctx context.Context, to, body string) (string, error) {
	form := url.Values{}
	form.Set("To", to)
	form.Set("Body", body)
	if c.messagingServiceSID != "" {
		form.Set("MessagingServiceSid", c.messagingServiceSID)
	} else {
		form.Set("From", c.fromNumber)
	}

	request, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return "", fmt.Errorf("build twilio request: %w", err)
	}
	request.SetBasicAuth(c.accountSID, c.authToken)
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	httpResponse, err := c.httpClient.Do(request)
	if err != nil {
		return "", fmt.Errorf("send twilio message: %w", err)
	}
	defer httpResponse.Body.Close()
	data, err := io.ReadAll(io.LimitReader(httpResponse.Body, 1<<20))
	if err != nil {
		return "", fmt.Errorf("read twilio response: %w", err)
	}
	var payload response
	if err := json.Unmarshal(data, &payload); err != nil {
		if httpResponse.StatusCode < 200 || httpResponse.StatusCode >= 300 {
			return "", classifyHTTPError(httpResponse.StatusCode, truncate(string(data), 512))
		}
		return "", fmt.Errorf("decode twilio response: %w", err)
	}
	if httpResponse.StatusCode < 200 || httpResponse.StatusCode >= 300 {
		message := payload.Message
		if message == "" {
			message = truncate(string(data), 512)
		}
		return "", classifyHTTPError(httpResponse.StatusCode, message)
	}
	if payload.SID == "" {
		return "", fmt.Errorf("twilio response does not contain a message sid")
	}
	return payload.SID, nil
}

func classifyHTTPError(statusCode int, message string) error {
	httpErr := &HTTPError{StatusCode: statusCode, Message: message}
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
