package messaging

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/nats-io/nats.go"
)

type Client struct {
	conn *nats.Conn
	js   nats.JetStreamContext
}

type StreamsConfig struct {
	SMSStream       string
	SMSSubject      string
	DLQStream       string
	DLQSubject      string
	MaxAge          time.Duration
	DuplicateWindow time.Duration
	MaxMsgs         int64
	MaxBytes        int64
}

type ConsumerConfig struct {
	Stream        string
	Subject       string
	Durable       string
	AckWait       time.Duration
	MaxDeliver    int
	MaxAckPending int
}

type ConnectConfig struct {
	URL    string
	Name   string
	Logger *slog.Logger
}

var ErrNoMessages = errors.New("no messages available")

type DeliveryInfo struct {
	Stream         string
	Consumer       string
	StreamSequence uint64
	Deliveries     uint64
	Pending        uint64
}

type Delivery interface {
	Data() []byte
	Info() DeliveryInfo
	Ack() error
	NakWithDelay(delay time.Duration) error
}

type delivery struct {
	message *nats.Msg
	info    DeliveryInfo
}

func (d *delivery) Data() []byte {
	return d.message.Data
}

func (d *delivery) Info() DeliveryInfo {
	return d.info
}

func (d *delivery) Ack() error {
	return d.message.Ack()
}

func (d *delivery) NakWithDelay(delay time.Duration) error {
	return d.message.NakWithDelay(delay)
}

type Consumer struct {
	subscription *nats.Subscription
	maxWait      time.Duration
}

func Connect(cfg ConnectConfig) (*Client, error) {
	options := []nats.Option{
		nats.Name(cfg.Name),
		nats.Timeout(5 * time.Second),
		nats.MaxReconnects(-1),
		nats.ReconnectWait(2 * time.Second),
	}
	if cfg.Logger != nil {
		options = append(options,
			nats.DisconnectErrHandler(func(_ *nats.Conn, err error) {
				cfg.Logger.Warn("nats disconnected", "error", err)
			}),
			nats.ReconnectHandler(func(conn *nats.Conn) {
				cfg.Logger.Info("nats reconnected", "url", conn.ConnectedUrl())
			}),
			nats.ErrorHandler(func(_ *nats.Conn, _ *nats.Subscription, err error) {
				cfg.Logger.Error("nats error", "error", err)
			}),
		)
	}
	conn, err := nats.Connect(cfg.URL, options...)
	if err != nil {
		return nil, fmt.Errorf("connect to nats: %w", err)
	}
	js, err := conn.JetStream(nats.MaxWait(10 * time.Second))
	if err != nil {
		conn.Close()
		return nil, fmt.Errorf("create jetstream context: %w", err)
	}
	return &Client{conn: conn, js: js}, nil
}

func (c *Client) EnsureStreams(ctx context.Context, cfg StreamsConfig) error {
	sms := &nats.StreamConfig{
		Name:       cfg.SMSStream,
		Subjects:   []string{cfg.SMSSubject},
		Retention:  nats.LimitsPolicy,
		Storage:    nats.FileStorage,
		Discard:    nats.DiscardOld,
		MaxAge:     cfg.MaxAge,
		Duplicates: cfg.DuplicateWindow,
		MaxMsgs:    cfg.MaxMsgs,
		MaxBytes:   cfg.MaxBytes,
		Replicas:   1,
	}
	dlq := &nats.StreamConfig{
		Name:       cfg.DLQStream,
		Subjects:   []string{cfg.DLQSubject},
		Retention:  nats.LimitsPolicy,
		Storage:    nats.FileStorage,
		Discard:    nats.DiscardOld,
		MaxAge:     cfg.MaxAge,
		Duplicates: cfg.DuplicateWindow,
		MaxMsgs:    cfg.MaxMsgs,
		MaxBytes:   cfg.MaxBytes,
		Replicas:   1,
	}
	if err := ensureStream(ctx, c.js, sms); err != nil {
		return err
	}
	if err := ensureStream(ctx, c.js, dlq); err != nil {
		return err
	}
	return nil
}

func ensureStream(ctx context.Context, js nats.JetStreamContext, cfg *nats.StreamConfig) error {
	info, err := js.StreamInfo(cfg.Name, nats.Context(ctx))
	if errors.Is(err, nats.ErrStreamNotFound) {
		if _, err := js.AddStream(cfg, nats.Context(ctx)); err != nil {
			return fmt.Errorf("create stream %s: %w", cfg.Name, err)
		}
		return nil
	}
	if err != nil {
		return fmt.Errorf("inspect stream %s: %w", cfg.Name, err)
	}
	if len(info.Config.Subjects) == len(cfg.Subjects) && len(info.Config.Subjects) == 1 && info.Config.Subjects[0] == cfg.Subjects[0] {
		if _, err := js.UpdateStream(cfg, nats.Context(ctx)); err != nil {
			return fmt.Errorf("update stream %s: %w", cfg.Name, err)
		}
		return nil
	}
	return fmt.Errorf("stream %s uses incompatible subjects", cfg.Name)
}

func (c *Client) Publish(ctx context.Context, subject, messageID string, data []byte) error {
	message := &nats.Msg{
		Subject: subject,
		Data:    data,
		Header:  nats.Header{},
	}
	message.Header.Set(nats.MsgIdHdr, messageID)
	ack, err := c.js.PublishMsg(message, nats.Context(ctx))
	if err != nil {
		return fmt.Errorf("publish jetstream message: %w", err)
	}
	if ack.Stream == "" {
		return errors.New("jetstream publish acknowledgement is empty")
	}
	return nil
}

func (c *Client) NewConsumer(ctx context.Context, cfg ConsumerConfig) (*Consumer, error) {
	if cfg.MaxDeliver < 1 {
		cfg.MaxDeliver = 1
	}
	if cfg.MaxAckPending < 1 {
		cfg.MaxAckPending = 1
	}
	consumerConfig := &nats.ConsumerConfig{
		Durable:       cfg.Durable,
		AckPolicy:     nats.AckExplicitPolicy,
		AckWait:       cfg.AckWait,
		MaxDeliver:    cfg.MaxDeliver,
		MaxAckPending: cfg.MaxAckPending,
		DeliverPolicy: nats.DeliverAllPolicy,
		ReplayPolicy:  nats.ReplayInstantPolicy,
		FilterSubject: cfg.Subject,
	}
	_, err := c.js.ConsumerInfo(cfg.Stream, cfg.Durable, nats.Context(ctx))
	if errors.Is(err, nats.ErrConsumerNotFound) {
		if _, err := c.js.AddConsumer(cfg.Stream, consumerConfig, nats.Context(ctx)); err != nil {
			return nil, fmt.Errorf("create pull consumer: %w", err)
		}
	} else if err != nil {
		return nil, fmt.Errorf("inspect pull consumer: %w", err)
	}
	subscription, err := c.js.PullSubscribe(
		cfg.Subject,
		cfg.Durable,
		nats.Bind(cfg.Stream, cfg.Durable),
		nats.ManualAck(),
	)
	if err != nil {
		return nil, fmt.Errorf("bind pull consumer: %w", err)
	}
	return &Consumer{subscription: subscription, maxWait: 2 * time.Second}, nil
}

func (c *Client) Ready(ctx context.Context) error {
	if !c.conn.IsConnected() {
		return errors.New("nats is not connected")
	}
	if err := c.conn.FlushWithContext(ctx); err != nil {
		return fmt.Errorf("flush nats connection: %w", err)
	}
	return nil
}

func (c *Client) Close() {
	_ = c.conn.Drain()
	c.conn.Close()
}

func (c *Consumer) SetMaxWait(wait time.Duration) {
	if wait > 0 {
		c.maxWait = wait
	}
}

func (c *Consumer) Fetch(ctx context.Context, batch int) ([]Delivery, error) {
	fetchCtx, cancel := context.WithTimeout(ctx, c.maxWait)
	defer cancel()
	messages, err := c.subscription.Fetch(batch, nats.Context(fetchCtx))
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		if errors.Is(err, nats.ErrTimeout) || errors.Is(err, context.DeadlineExceeded) {
			return nil, ErrNoMessages
		}
		return nil, fmt.Errorf("fetch jetstream messages: %w", err)
	}
	deliveries := make([]Delivery, 0, len(messages))
	for _, message := range messages {
		info := DeliveryInfo{}
		if metadata, err := message.Metadata(); err == nil {
			info.Stream = metadata.Stream
			info.Consumer = metadata.Consumer
			info.StreamSequence = metadata.Sequence.Stream
			info.Deliveries = metadata.NumDelivered
			info.Pending = metadata.NumPending
		}
		deliveries = append(deliveries, &delivery{message: message, info: info})
	}
	return deliveries, nil
}

func (c *Consumer) Close() error {
	return c.subscription.Unsubscribe()
}
