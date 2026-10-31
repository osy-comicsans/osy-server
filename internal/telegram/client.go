package telegram

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

type Config struct {
	BotToken    string
	BaseURL     string
	HTTPTimeout time.Duration
}

type Client struct {
	token   string
	baseURL string
	http    *http.Client
}

type sendRequest struct {
	ChatID                string `json:"chat_id"`
	Text                  string `json:"text"`
	DisableWebPagePreview bool   `json:"disable_web_page_preview"`
}

type apiResponse struct {
	OK          bool   `json:"ok"`
	Description string `json:"description"`
	ErrorCode   int    `json:"error_code"`
	Result      struct {
		MessageID int64 `json:"message_id"`
	} `json:"result"`
}

type HTTPError struct {
	StatusCode int
	Message    string
}

func (e *HTTPError) Error() string {
	return fmt.Sprintf("telegram returned status %d: %s", e.StatusCode, e.Message)
}

func NewClient(cfg Config) (*Client, error) {
	if strings.TrimSpace(cfg.BotToken) == "" {
		return nil, fmt.Errorf("telegram bot token is required")
	}
	if cfg.BaseURL == "" {
		cfg.BaseURL = "https://api.telegram.org"
	}
	parsed, err := url.Parse(cfg.BaseURL)
	if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return nil, fmt.Errorf("invalid telegram API base URL")
	}
	if cfg.HTTPTimeout <= 0 {
		cfg.HTTPTimeout = 20 * time.Second
	}
	return &Client{
		token:   cfg.BotToken,
		baseURL: strings.TrimRight(cfg.BaseURL, "/"),
		http:    &http.Client{Timeout: cfg.HTTPTimeout},
	}, nil
}

func (c *Client) Send(ctx context.Context, chatID, body string) (string, error) {
	payload, err := json.Marshal(sendRequest{
		ChatID:                chatID,
		Text:                  body,
		DisableWebPagePreview: true,
	})
	if err != nil {
		return "", fmt.Errorf("encode telegram request: %w", err)
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/bot"+c.token+"/sendMessage", bytes.NewReader(payload))
	if err != nil {
		return "", fmt.Errorf("build telegram request: %w", err)
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "application/json")
	response, err := c.http.Do(request)
	if err != nil {
		return "", fmt.Errorf("send telegram message: %w", err)
	}
	defer response.Body.Close()
	data, err := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	if err != nil {
		return "", fmt.Errorf("read telegram response: %w", err)
	}
	var result apiResponse
	if err := json.Unmarshal(data, &result); err != nil {
		return "", fmt.Errorf("decode telegram response: %w", err)
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 || !result.OK {
		message := result.Description
		if message == "" {
			message = truncate(string(data), 512)
		}
		return "", classifyHTTPError(response.StatusCode, message)
	}
	if result.Result.MessageID == 0 {
		return "", fmt.Errorf("telegram response does not contain a message id")
	}
	return "telegram-" + strconv.FormatInt(result.Result.MessageID, 10), nil
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
