## Core API

```go
backend.Publish(ctx, "events.user.created", pulse.Message{Data: data}, pulse.PublishOptions{})

sub, err := backend.Subscribe(ctx, "events.user.created", func(ctx context.Context, msg pulse.Message) error {
    return nil
}, pulse.SubscribeOptions{Delivery: pulse.Fanout})
_ = sub.Close(ctx)

resp, err := backend.Request(ctx, "acme.UserService/GetUser", pulse.Message{Data: reqBytes}, pulse.RequestOptions{
    Timeout: 2 * time.Second,
})

future, err := backend.RequestAsync(ctx, "acme.UserService/GetUser", pulse.Message{Data: reqBytes}, pulse.RequestOptions{
    Timeout: 2 * time.Second,
})
// do other work
resp, err = future.Await(ctx)
```

## ConnectRPC generation

Generate normal protobuf and Connect code, then run the Pulse plugin:

```yaml
# buf.gen.yaml
version: v2
plugins:
  - remote: buf.build/protocolbuffers/go
    out: gen
    opt: paths=source_relative
  - remote: buf.build/connectrpc/go
    out: gen
    opt: paths=source_relative
  - local: protoc-gen-pulse-connect
    out: gen
    opt:
      - paths=source_relative
      - pulse_pkg=github.com/omnitrust/pulse
      - rpc_pkg=github.com/omnitrust/pulse/rpc
```

The generated Pulse client has the same unary method signatures as Connect's generated client interface:

```go
client := userv1.NewUserServicePulseClient(backend)
resp, err := client.GetUser(ctx, connect.NewRequest(&userv1.GetUserRequest{Id: "123"}))
```

The generated server registration accepts any handler with the same method set as the Connect-generated handler interface:

```go
server, err := userv1.RegisterUserServicePulseHandler(ctx, backend, myConnectHandler)
```