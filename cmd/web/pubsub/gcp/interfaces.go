package gcp

import (
	"context"

	gcppubsub "cloud.google.com/go/pubsub"
)

type Client interface {
	Topic(id string) Topic
	Subscription(id string) *gcppubsub.Subscription
	CreateSubscription(ctx context.Context, id string, cfg gcppubsub.SubscriptionConfig) (*gcppubsub.Subscription, error)
	Close() error
}

type Topic interface {
	Exists(ctx context.Context) (bool, error)
	Publish(ctx context.Context, msg *gcppubsub.Message) *gcppubsub.PublishResult
	Stop()
}

type Subscription interface {
	String() string
	ID() string
	Delete(ctx context.Context) error
	Exists(ctx context.Context) (bool, error)
	Receive(ctx context.Context, f func(context.Context, *gcppubsub.Message)) error
}

type Message interface {
	Ack()
	Nack()
}
