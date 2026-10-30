package africastalking

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"osyn/internal/retry"
)

type SMSMessage struct {
	ID     string
	From   string
	To     string
	Text   string
	Date   time.Time
	LinkID string
}

type SMSProvider interface {
	FetchMessages(ctx context.Context, lastReceivedID string) ([]SMSMessage, error)
}

type Client struct {
	httpClient *http.Client
	baseURL    string
	username   string
	apiKey     string
	retry      retry.Policy
}

type Config struct {
	BaseURL     string
	Username    string
	APIKey      string
	HTTPTimeout time.Duration
	Retry       retry.Policy
}

type HTTPError struct {
	StatusCode int
	Body       string
}

func (e *HTTPError) Error() string {
	return fmt.Sprintf("africastalking returned status %d: %s", e.StatusCode, e.Body)
}

type scalarString string

func (s *scalarString) UnmarshalJSON(data []byte) error {
	if bytes.Equal(data, []byte("null")) {
		*s = ""
		return nil
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		return err
	}
	switch typed := value.(type) {
	case string:
		*s = scalarString(typed)
	case json.Number:
		*s = scalarString(typed.String())
	case bool:
		*s = scalarString(strconv.FormatBool(typed))
	default:
		return fmt.Errorf("unsupported scalar value %T", value)
	}
	return nil
}

type fetchResponse struct {
	SMSMessageData struct {
		Messages []struct {
			ID     scalarString `json:"id"`
			From   string       `json:"from"`
			To     string       `json:"to"`
			Text   string       `json:"text"`
			Date   string       `json:"date"`
			LinkID scalarString `json:"linkId"`
		} `json:"Messages"`
	} `json:"SMSMessageData"`
}

func NewClient(cfg Config) (*Client, error) {
	if strings.TrimSpace(cfg.BaseURL) == "" {
		return nil, fmt.Errorf("africastalking base URL is required")
	}
	if _, err := url.Parse(cfg.BaseURL); err != nil {
		return nil, fmt.Errorf("parse africastalking base URL: %w", err)
	}
	if cfg.HTTPTimeout <= 0 {
		cfg.HTTPTimeout = 15 * time.Second
	}
	return &Client{
		httpClient: &http.Client{Timeout: cfg.HTTPTimeout},
		baseURL:    strings.TrimRight(cfg.BaseURL, "/"),
		username:   cfg.Username,
		apiKey:     cfg.APIKey,
		retry:      cfg.Retry,
	}, nil
}

func (c *Client) FetchMessages(ctx context.Context, lastReceivedID string) ([]SMSMessage, error) {
	var messages []SMSMessage
	err := c.retry.Do(ctx, func(ctx context.Context) error {
		fetched, err := c.fetch(ctx, lastReceivedID)
		if err != nil {
			return err
		}
		messages = fetched
		return nil
	})
	if err != nil {
		return nil, err
	}
	return messages, nil
}

func (c *Client) fetch(ctx context.Context, lastReceivedID string) ([]SMSMessage, error) {
	endpoint, err := url.Parse(c.baseURL + "/version1/messaging")
	if err != nil {
		return nil, retry.Permanent(fmt.Errorf("build messaging URL: %w", err))
	}
	query := endpoint.Query()
	query.Set("username", c.username)
	query.Set("lastReceivedId", lastReceivedID)
	endpoint.RawQuery = query.Encode()

	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint.String(), nil)
	if err != nil {
		return nil, retry.Permanent(fmt.Errorf("build messaging request: %w", err))
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("apiKey", c.apiKey)

	response, err := c.httpClient.Do(request)
	if err != nil {
		return nil, fmt.Errorf("fetch messages: %w", err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(io.LimitReader(response.Body, 4<<20))
	if err != nil {
		return nil, fmt.Errorf("read messages response: %w", err)
	}
	if response.StatusCode == http.StatusTooManyRequests || response.StatusCode >= http.StatusInternalServerError || response.StatusCode == http.StatusRequestTimeout {
		return nil, &HTTPError{StatusCode: response.StatusCode, Body: truncate(string(body), 512)}
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, retry.Permanent(&HTTPError{StatusCode: response.StatusCode, Body: truncate(string(body), 512)})
	}

	var payload fetchResponse
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, retry.Permanent(fmt.Errorf("decode messages response: %w", err))
	}
	messages := make([]SMSMessage, 0, len(payload.SMSMessageData.Messages))
	for _, raw := range payload.SMSMessageData.Messages {
		id := string(raw.ID)
		if strings.TrimSpace(id) == "" {
			return nil, retry.Permanent(fmt.Errorf("africastalking response contains a message without an id"))
		}
		receivedAt, err := parseDate(raw.Date)
		if err != nil {
			return nil, retry.Permanent(fmt.Errorf("parse message %s date: %w", id, err))
		}
		messages = append(messages, SMSMessage{
			ID:     id,
			From:   raw.From,
			To:     raw.To,
			Text:   raw.Text,
			Date:   receivedAt,
			LinkID: string(raw.LinkID),
		})
	}
	return messages, nil
}

func parseDate(value string) (time.Time, error) {
	formats := []string{
		time.RFC3339Nano,
		"2006-01-02 15:04:05",
		"2006-01-02T15:04:05",
	}
	for _, format := range formats {
		if parsed, err := time.Parse(format, value); err == nil {
			return parsed, nil
		}
	}
	return time.Time{}, fmt.Errorf("unsupported date %q", value)
}

func truncate(value string, limit int) string {
	if len(value) <= limit {
		return value
	}
	return value[:limit]
}
