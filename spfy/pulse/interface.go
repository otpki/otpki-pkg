package pulse

import "context"

type Handler func(context.Context, Message) error

type RequestHandler func(context.Context, Message) (Message, error)

type Subscription interface {
	Close(context.Context) error
}

type Backend interface {
	Publish(context.Context, string, Message, PublishOptions) error
	Subscribe(context.Context, string, Handler, SubscribeOptions) (Subscription, error)

	Request(context.Context, string, Message, RequestOptions) (Message, error)
	RequestAsync(context.Context, string, Message, RequestOptions) (Future[Message], error)
	Handle(context.Context, string, RequestHandler, SubscribeOptions) (Subscription, error)

	Shutdown(context.Context) error
	Capabilities() Capabilities
}
