// Package amqp provides a client with included tracing capabilities.
//
// Deprecated: The AMQP client package is superseded by the `github.com/beatlabs/client/amqp/v2` package.
// Please refer to the documents and the examples for the usage.
//
// This package is frozen and no new functionality will be added.
package amqp

import (
	"context"
	"errors"
	"fmt"
	"net"
	"time"

	"github.com/beatlabs/patron/correlation"
	"github.com/beatlabs/patron/encoding/json"
	"github.com/beatlabs/patron/encoding/protobuf"
	patronErrors "github.com/beatlabs/patron/errors"
	"github.com/beatlabs/patron/trace"
	"github.com/opentracing/opentracing-go"
	"github.com/opentracing/opentracing-go/ext"
	"github.com/streadway/amqp"
)

const (
	publisherComponent = "amqp-publisher"
)

// Message abstraction for publishing.
type Message struct {
	contentType string
	body        []byte
}

// NewMessage creates a new message.
func NewMessage(ct string, body []byte) *Message {
	return &Message{contentType: ct, body: body}
}

// NewJSONMessage creates a new message with a JSON encoded body.
func NewJSONMessage(d interface{}) (*Message, error) {
	body, err := json.Encode(d)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal to JSON: %w", err)
	}
	return &Message{contentType: json.Type, body: body}, nil
}

// NewProtobufMessage creates a new message with a protobuf encoded body.
func NewProtobufMessage(d interface{}) (*Message, error) {
	body, err := protobuf.Encode(d)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal to protobuf: %w", err)
	}
	return &Message{contentType: protobuf.Type, body: body}, nil
}

// Publisher interface of a RabbitMQ publisher.
type Publisher interface {
	Publish(ctx context.Context, msg *Message) error
	Close(ctx context.Context) error
}

var defaultCfg = amqp.Config{
	Dial: func(network, addr string) (net.Conn, error) {
		return net.DialTimeout(network, addr, 30*time.Second)
	},
}

// TracedPublisher defines a RabbitMQ publisher with tracing instrumentation.
type TracedPublisher struct {
	cfg amqp.Config
	cn  *amqp.Connection
	ch  *amqp.Channel
	exc string
	tag opentracing.Tag
}

// NewPublisher creates a new publisher with the following defaults
// - exchange type: fanout
// - notifications are not handled at this point TBD.
//
// Deprecated: The AMQP client package is superseded by the `github.com/beatlabs/client/amqp/v2` package.
// Please refer to the documents and the examples for the usage.
//
// This package is frozen and no new functionality will be added.
func NewPublisher(url, exc string, oo ...OptionFunc) (*TracedPublisher, error) {
	if url == "" {
		return nil, errors.New("url is required")
	}

	if exc == "" {
		return nil, errors.New("exchange is required")
	}

	p := TracedPublisher{
		cfg: defaultCfg,
		exc: exc,
		tag: opentracing.Tag{Key: "exchange", Value: exc},
	}

	for _, o := range oo {
		err := o(&p)
		if err != nil {
			return nil, err
		}
	}

	conn, err := amqp.DialConfig(url, p.cfg)
	if err != nil {
		return nil, fmt.Errorf("failed to open RabbitMq connection: %w", err)
	}
	p.cn = conn

	ch, err := conn.Channel()
	if err != nil {
		return nil, fmt.Errorf("failed to open RabbitMq channel: %w", err)
	}
	p.ch = ch

	err = ch.ExchangeDeclare(exc, amqp.ExchangeFanout, true, false, false, false, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to declare exchange: %w", err)
	}

	return &p, nil
}

// Publish a message to an exchange.
func (tc *TracedPublisher) Publish(ctx context.Context, msg *Message) error {
	sp, _ := trace.ChildSpan(ctx, trace.ComponentOpName(publisherComponent, tc.exc),
		publisherComponent, ext.SpanKindProducer, tc.tag)

	p := amqp.Publishing{
		Headers:     amqp.Table{},
		ContentType: msg.contentType,
		Body:        msg.body,
	}

	c := amqpHeadersCarrier(p.Headers)
	err := sp.Tracer().Inject(sp.Context(), opentracing.TextMap, c)
	if err != nil {
		return fmt.Errorf("failed to inject tracing headers: %w", err)
	}
	p.Headers[correlation.HeaderID] = correlation.IDFromContext(ctx)

	err = tc.ch.Publish(tc.exc, "", false, false, p)
	trace.SpanComplete(sp, err)
	if err != nil {
		return fmt.Errorf("failed to publish message: %w", err)
	}

	return nil
}

// Close the connection and channel of the publisher.
func (tc *TracedPublisher) Close(_ context.Context) error {
	return patronErrors.Aggregate(tc.ch.Close(), tc.cn.Close())
}

type amqpHeadersCarrier map[string]interface{}

// Set implements Set() of opentracing.TextMapWriter.
func (c amqpHeadersCarrier) Set(key, val string) {
	c[key] = val
}
