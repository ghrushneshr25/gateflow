package kafkax

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/segmentio/kafka-go"
)

const TopicOrderConfirmed = "gateflow.order.order.confirmed"

// Producer is a thin Kafka writer wrapper.
type Producer struct {
	w *kafka.Writer
}

func NewProducer(brokers []string) *Producer {
	return &Producer{w: &kafka.Writer{
		Addr:         kafka.TCP(brokers...),
		Balancer:     &kafka.Hash{},
		RequiredAcks: kafka.RequireOne,
		Async:        false,
		BatchTimeout: 10 * time.Millisecond,
	}}
}

func (p *Producer) Publish(ctx context.Context, topic, key string, headers map[string]string, value []byte) error {
	h := make([]kafka.Header, 0, len(headers))
	for k, v := range headers {
		h = append(h, kafka.Header{Key: k, Value: []byte(v)})
	}
	return p.w.WriteMessages(ctx, kafka.Message{
		Topic:   topic,
		Key:     []byte(key),
		Value:   value,
		Headers: h,
		Time:    time.Now().UTC(),
	})
}

func (p *Producer) Close() error {
	if p == nil || p.w == nil {
		return nil
	}
	return p.w.Close()
}

// Message is a fetched Kafka record before commit.
type Message struct {
	Key     []byte
	Value   []byte
	Headers map[string]string
	raw     kafka.Message
}

// Consumer reads a topic with a consumer group. Commit is explicit after successful processing.
type Consumer struct {
	r *kafka.Reader
}

func NewConsumer(brokers []string, topic, group string) *Consumer {
	start := kafka.FirstOffset
	if os.Getenv("GATEFLOW_KAFKA_START") == "latest" {
		start = kafka.LastOffset
	}
	return &Consumer{r: kafka.NewReader(kafka.ReaderConfig{
		Brokers:        brokers,
		Topic:          topic,
		GroupID:        group,
		MinBytes:       1,
		MaxBytes:       10e6,
		CommitInterval: 0, // manual commit
		StartOffset:    start,
	})}
}

func (c *Consumer) Fetch(ctx context.Context) (Message, error) {
	m, err := c.r.FetchMessage(ctx)
	if err != nil {
		return Message{}, err
	}
	h := map[string]string{}
	for _, hdr := range m.Headers {
		h[hdr.Key] = string(hdr.Value)
	}
	return Message{Key: m.Key, Value: m.Value, Headers: h, raw: m}, nil
}

func (c *Consumer) Commit(ctx context.Context, msg Message) error {
	return c.r.CommitMessages(ctx, msg.raw)
}

func (c *Consumer) Close() error {
	if c == nil || c.r == nil {
		return nil
	}
	return c.r.Close()
}

// EnsureTopic creates topic if missing (local/dev helper).
func EnsureTopic(brokers []string, topic string, partitions int) error {
	if partitions <= 0 {
		partitions = 1
	}
	conn, err := kafka.Dial("tcp", brokers[0])
	if err != nil {
		return err
	}
	defer conn.Close()
	controller, err := conn.Controller()
	if err != nil {
		return err
	}
	controllerConn, err := kafka.Dial("tcp", fmt.Sprintf("%s:%d", controller.Host, controller.Port))
	if err != nil {
		return err
	}
	defer controllerConn.Close()
	err = controllerConn.CreateTopics(kafka.TopicConfig{
		Topic:             topic,
		NumPartitions:     partitions,
		ReplicationFactor: 1,
	})
	if err != nil {
		es := err.Error()
		if strings.Contains(es, "already exists") || strings.Contains(es, "TopicAlreadyExists") {
			return nil
		}
		return err
	}
	return nil
}

func ParseBrokers(csv string) []string {
	parts := strings.Split(csv, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	if len(out) == 0 {
		return []string{"localhost:9092"}
	}
	return out
}
