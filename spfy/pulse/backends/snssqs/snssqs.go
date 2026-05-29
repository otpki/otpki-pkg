package snssqs

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/sns"
	snstypes "github.com/aws/aws-sdk-go-v2/service/sns/types"
	"github.com/aws/aws-sdk-go-v2/service/sqs"
	sqstypes "github.com/aws/aws-sdk-go-v2/service/sqs/types"
	"github.com/otpki/otpki-pkg/spfy/pulse"
	"github.com/otpki/otpki-pkg/spfy/pulse/internal/ids"
	"github.com/samber/oops"
)

type SNSAPI interface {
	CreateTopic(context.Context, *sns.CreateTopicInput, ...func(*sns.Options)) (*sns.CreateTopicOutput, error)
	Publish(context.Context, *sns.PublishInput, ...func(*sns.Options)) (*sns.PublishOutput, error)
	Subscribe(context.Context, *sns.SubscribeInput, ...func(*sns.Options)) (*sns.SubscribeOutput, error)
}
type SQSAPI interface {
	CreateQueue(context.Context, *sqs.CreateQueueInput, ...func(*sqs.Options)) (*sqs.CreateQueueOutput, error)
	GetQueueAttributes(context.Context, *sqs.GetQueueAttributesInput, ...func(*sqs.Options)) (*sqs.GetQueueAttributesOutput, error)
	SetQueueAttributes(context.Context, *sqs.SetQueueAttributesInput, ...func(*sqs.Options)) (*sqs.SetQueueAttributesOutput, error)
	ReceiveMessage(context.Context, *sqs.ReceiveMessageInput, ...func(*sqs.Options)) (*sqs.ReceiveMessageOutput, error)
	DeleteMessage(context.Context, *sqs.DeleteMessageInput, ...func(*sqs.Options)) (*sqs.DeleteMessageOutput, error)
}

type Backend struct {
	sns      SNSAPI
	sqs      SQSAPI
	cfg      Config
	closed   atomic.Bool
	mu       sync.Mutex
	topics   map[string]string
	queues   map[string]queueInfo
	bindings map[string]struct{}
	subs     map[*subscription]struct{}
}
type queueInfo struct {
	URL string
	ARN string
}

func New(snsClient SNSAPI, sqsClient SQSAPI, cfg Config) (*Backend, error) {
	if snsClient == nil || sqsClient == nil {
		return nil, oops.New("pulse snssqs: nil sns or sqs client")
	}
	return &Backend{sns: snsClient, sqs: sqsClient, cfg: normalizeConfig(cfg), topics: map[string]string{}, queues: map[string]queueInfo{}, bindings: map[string]struct{}{}, subs: map[*subscription]struct{}{}}, nil
}
func (b *Backend) Capabilities() pulse.Capabilities {
	return pulse.Capabilities{Fanout: true, WorkQueue: true, DurablePubSub: true, DurableRPC: true}
}
func (b *Backend) Shutdown(ctx context.Context) error {
	if b.closed.Swap(true) {
		return nil
	}
	b.mu.Lock()
	subs := make([]*subscription, 0, len(b.subs))
	for s := range b.subs {
		subs = append(subs, s)
	}
	b.subs = map[*subscription]struct{}{}
	b.mu.Unlock()
	for _, s := range subs {
		_ = s.Close(ctx)
	}
	return ctx.Err()
}
func (b *Backend) Publish(ctx context.Context, subject string, msg pulse.Message, opts pulse.PublishOptions) error {
	return b.publish(ctx, pulse.Envelope{ID: ids.New(), Subject: subject, Message: msg.Clone(), Timestamp: time.Now().UTC()})
}
func (b *Backend) publish(ctx context.Context, env pulse.Envelope) error {
	if env.Subject == "" {
		return pulse.ErrInvalidName
	}
	if b.closed.Load() {
		return pulse.ErrClosed
	}
	arn, err := b.topicARN(ctx, env.Subject)
	if err != nil {
		return err
	}
	body, err := json.Marshal(wireEnvelope{ID: env.ID, Subject: env.Subject, Timestamp: env.Timestamp.Format(time.RFC3339Nano), CorrelationID: env.CorrelationID, ReplyTo: env.ReplyTo, Header: env.Message.Header, Data: env.Message.Data})
	if err != nil {
		return err
	}
	_, err = b.sns.Publish(ctx, &sns.PublishInput{TopicArn: aws.String(arn), Message: aws.String(string(body)), MessageAttributes: map[string]snstypes.MessageAttributeValue{"pulse-id": {DataType: aws.String("String"), StringValue: aws.String(env.ID)}}})
	return err
}
func (b *Backend) Subscribe(ctx context.Context, subject string, handler pulse.Handler, opts pulse.SubscribeOptions) (pulse.Subscription, error) {
	if subject == "" || handler == nil {
		return nil, pulse.ErrInvalidName
	}
	return b.subscribe(ctx, subject, func(ctx context.Context, msg pulse.Message) (pulse.Message, error) {
		return pulse.Message{}, handler(ctx, msg)
	}, opts, false)
}
func (b *Backend) Handle(ctx context.Context, subject string, handler pulse.RequestHandler, opts pulse.SubscribeOptions) (pulse.Subscription, error) {
	if subject == "" || handler == nil {
		return nil, pulse.ErrInvalidName
	}
	opts.Delivery = pulse.WorkQueue
	return b.subscribe(ctx, subject, handler, opts, true)
}
func (b *Backend) subscribe(ctx context.Context, subject string, handler pulse.RequestHandler, opts pulse.SubscribeOptions, rpc bool) (pulse.Subscription, error) {
	if b.closed.Load() {
		return nil, pulse.ErrClosed
	}
	opts = pulse.NormalizeSubscribeOptions(subject, opts)
	topicARN, err := b.topicARN(ctx, subject)
	if err != nil {
		return nil, err
	}
	q, err := b.queue(ctx, subject, opts, rpc)
	if err != nil {
		return nil, err
	}
	if err := b.bind(ctx, topicARN, q); err != nil {
		return nil, err
	}
	s := &subscription{backend: b, queue: q, closed: make(chan struct{}), rpc: rpc}
	b.mu.Lock()
	b.subs[s] = struct{}{}
	b.mu.Unlock()
	go s.receive(ctx, handler)
	return s, nil
}
func (b *Backend) Request(ctx context.Context, subject string, req pulse.Message, opts pulse.RequestOptions) (pulse.Message, error) {
	f, err := b.RequestAsync(ctx, subject, req, opts)
	if err != nil {
		return pulse.Message{}, err
	}
	return f.Await(ctx)
}
func (b *Backend) RequestAsync(ctx context.Context, subject string, req pulse.Message, opts pulse.RequestOptions) (pulse.Future[pulse.Message], error) {
	if subject == "" {
		return pulse.Future[pulse.Message]{}, pulse.ErrInvalidName
	}
	if b.closed.Load() {
		return pulse.Future[pulse.Message]{}, pulse.ErrClosed
	}
	ch := make(chan pulse.Result[pulse.Message], 1)
	corr := ids.New()
	if opts.NoReply {
		go func() {
			defer close(ch)
			ch <- pulse.Result[pulse.Message]{Err: b.publish(ctx, pulse.Envelope{ID: ids.New(), Subject: subject, Message: req.Clone(), Timestamp: time.Now().UTC(), CorrelationID: corr})}
		}()
		return pulse.NewFuture(ch), nil
	}
	reply := b.cfg.ReplyPrefix + pulse.SafeName(corr)
	replies := make(chan pulse.Message, 1)
	sub, err := b.Subscribe(ctx, reply, func(ctx context.Context, msg pulse.Message) error {
		select {
		case replies <- msg.Clone():
		default:
		}
		return nil
	}, pulse.SubscribeOptions{Delivery: pulse.WorkQueue, Group: corr, Reliability: opts.Reliability})
	if err != nil {
		return pulse.Future[pulse.Message]{}, err
	}
	if err := b.publish(ctx, pulse.Envelope{ID: ids.New(), Subject: subject, Message: req.Clone(), Timestamp: time.Now().UTC(), CorrelationID: corr, ReplyTo: reply}); err != nil {
		_ = sub.Close(context.Background())
		return pulse.Future[pulse.Message]{}, err
	}
	go func() {
		defer close(ch)
		defer sub.Close(context.Background())
		callCtx := ctx
		var cancel context.CancelFunc
		if _, ok := callCtx.Deadline(); !ok && opts.Timeout > 0 {
			callCtx, cancel = context.WithTimeout(callCtx, opts.Timeout)
		}
		if cancel != nil {
			defer cancel()
		}
		select {
		case msg := <-replies:
			ch <- pulse.Result[pulse.Message]{Value: msg}
		case <-callCtx.Done():
			ch <- pulse.Result[pulse.Message]{Err: callCtx.Err()}
		}
	}()
	return pulse.NewFuture(ch), nil
}
func (b *Backend) topicARN(ctx context.Context, subject string) (string, error) {
	name := b.name(subject)
	b.mu.Lock()
	if arn := b.topics[name]; arn != "" {
		b.mu.Unlock()
		return arn, nil
	}
	b.mu.Unlock()
	out, err := b.sns.CreateTopic(ctx, &sns.CreateTopicInput{Name: aws.String(name)})
	if err != nil {
		return "", err
	}
	arn := aws.ToString(out.TopicArn)
	b.mu.Lock()
	b.topics[name] = arn
	b.mu.Unlock()
	return arn, nil
}
func (b *Backend) queue(ctx context.Context, subject string, opts pulse.SubscribeOptions, rpc bool) (queueInfo, error) {
	name := b.queueName(subject, opts, rpc)
	b.mu.Lock()
	if q, ok := b.queues[name]; ok {
		b.mu.Unlock()
		return q, nil
	}
	b.mu.Unlock()
	out, err := b.sqs.CreateQueue(ctx, &sqs.CreateQueueInput{QueueName: aws.String(name), Attributes: map[string]string{string(sqstypes.QueueAttributeNameVisibilityTimeout): fmt.Sprint(b.cfg.VisibilityTimeout)}})
	if err != nil {
		return queueInfo{}, err
	}
	url := aws.ToString(out.QueueUrl)
	attrs, err := b.sqs.GetQueueAttributes(ctx, &sqs.GetQueueAttributesInput{QueueUrl: aws.String(url), AttributeNames: []sqstypes.QueueAttributeName{sqstypes.QueueAttributeNameQueueArn}})
	if err != nil {
		return queueInfo{}, err
	}
	q := queueInfo{URL: url, ARN: attrs.Attributes[string(sqstypes.QueueAttributeNameQueueArn)]}
	b.mu.Lock()
	b.queues[name] = q
	b.mu.Unlock()
	return q, nil
}
func (b *Backend) bind(ctx context.Context, topicARN string, q queueInfo) error {
	key := topicARN + "|" + q.ARN
	b.mu.Lock()
	if _, ok := b.bindings[key]; ok {
		b.mu.Unlock()
		return nil
	}
	b.mu.Unlock()
	policy := fmt.Sprintf(`{"Version":"2012-10-17","Statement":[{"Effect":"Allow","Principal":"*","Action":"sqs:SendMessage","Resource":%q,"Condition":{"ArnEquals":{"aws:SourceArn":%q}}}]}`, q.ARN, topicARN)
	_, _ = b.sqs.SetQueueAttributes(ctx, &sqs.SetQueueAttributesInput{QueueUrl: aws.String(q.URL), Attributes: map[string]string{string(sqstypes.QueueAttributeNamePolicy): policy}})
	_, err := b.sns.Subscribe(ctx, &sns.SubscribeInput{TopicArn: aws.String(topicARN), Protocol: aws.String("sqs"), Endpoint: aws.String(q.ARN), Attributes: map[string]string{"RawMessageDelivery": "true"}})
	if err != nil {
		return err
	}
	b.mu.Lock()
	b.bindings[key] = struct{}{}
	b.mu.Unlock()
	return nil
}
func (b *Backend) name(subject string) string {
	return limitName(awsName(b.cfg.NamePrefix+"-"+subject), 80)
}

func (b *Backend) queueName(subject string, opts pulse.SubscribeOptions, rpc bool) string {
	base := awsName(b.cfg.NamePrefix + "-" + subject)
	if opts.Delivery == pulse.Fanout && !rpc && opts.Reliability != pulse.Durable {
		return limitName(base+"-"+ids.New(), 80)
	}
	suffix := pulse.SafeName(opts.Group)
	if rpc {
		suffix = "rpc"
	}
	return limitName(base+"-"+strings.ReplaceAll(suffix, ".", "_"), 80)
}

func awsName(s string) string {
	var out strings.Builder
	lastDash := false
	for _, r := range s {
		valid := (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_'
		if valid {
			out.WriteRune(r)
			lastDash = false
			continue
		}
		if !lastDash {
			out.WriteByte('-')
			lastDash = true
		}
	}
	name := strings.Trim(out.String(), "-_")
	if name == "" {
		return "pulse"
	}
	return name
}

func limitName(name string, max int) string {
	name = strings.Trim(name, "-_")
	if len(name) <= max {
		if name == "" {
			return "pulse"
		}
		return name
	}
	sum := sha1.Sum([]byte(name))
	suffix := hex.EncodeToString(sum[:])[:12]
	keep := max - len(suffix) - 1
	if keep < 1 {
		return suffix[:max]
	}
	prefix := strings.Trim(name[:keep], "-_")
	if prefix == "" {
		prefix = "pulse"
	}
	return prefix + "-" + suffix
}

type wireEnvelope struct {
	ID            string       `json:"id"`
	Subject       string       `json:"subject"`
	Timestamp     string       `json:"ts"`
	CorrelationID string       `json:"cid"`
	ReplyTo       string       `json:"reply"`
	Header        pulse.Header `json:"header"`
	Data          []byte       `json:"data"`
}

func decode(body string) (pulse.Envelope, error) {
	var w wireEnvelope
	if err := json.Unmarshal([]byte(body), &w); err != nil {
		return pulse.Envelope{}, err
	}
	ts := time.Now().UTC()
	if w.Timestamp != "" {
		if parsed, err := time.Parse(time.RFC3339Nano, w.Timestamp); err == nil {
			ts = parsed
		}
	}
	return pulse.Envelope{ID: w.ID, Subject: w.Subject, Message: pulse.Message{Header: w.Header, Data: w.Data}, Timestamp: ts, CorrelationID: w.CorrelationID, ReplyTo: w.ReplyTo}, nil
}

type subscription struct {
	backend *Backend
	queue   queueInfo
	closed  chan struct{}
	once    sync.Once
	rpc     bool
}

func (s *subscription) Close(ctx context.Context) error {
	s.once.Do(func() { close(s.closed); s.backend.mu.Lock(); delete(s.backend.subs, s); s.backend.mu.Unlock() })
	return ctx.Err()
}
func (s *subscription) receive(ctx context.Context, handler pulse.RequestHandler) {
	for {
		select {
		case <-s.closed:
			return
		default:
		}
		out, err := s.backend.sqs.ReceiveMessage(ctx, &sqs.ReceiveMessageInput{QueueUrl: aws.String(s.queue.URL), MaxNumberOfMessages: s.backend.cfg.MaxMessages, WaitTimeSeconds: s.backend.cfg.WaitTimeSeconds, VisibilityTimeout: s.backend.cfg.VisibilityTimeout})
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			time.Sleep(s.backend.cfg.ReceiveBackoff)
			continue
		}
		for _, m := range out.Messages {
			env, err := decode(aws.ToString(m.Body))
			if err == nil {
				resp, herr := handler(ctx, env.Message)
				err = herr
				if err == nil && s.rpc && env.ReplyTo != "" {
					pubCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
					err = s.backend.publish(pubCtx, pulse.Envelope{ID: ids.New(), Subject: env.ReplyTo, Message: resp.Clone(), Timestamp: time.Now().UTC(), CorrelationID: env.CorrelationID})
					cancel()
				}
			}
			if err == nil {
				_, _ = s.backend.sqs.DeleteMessage(ctx, &sqs.DeleteMessageInput{QueueUrl: aws.String(s.queue.URL), ReceiptHandle: m.ReceiptHandle})
			}
		}
	}
}

var _ pulse.Backend = (*Backend)(nil)
var _ pulse.Subscription = (*subscription)(nil)
