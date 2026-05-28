package yuanxin

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/apache/pulsar-client-go/pulsar"

	"github.com/hrygo/hotplex/internal/messaging"
	"github.com/hrygo/hotplex/internal/session"
	"github.com/hrygo/hotplex/pkg/events"
)

func init() {
	messaging.Register(messaging.PlatformYuanxin, func(log *slog.Logger) messaging.PlatformAdapterInterface {
		return &Adapter{
			BaseAdapter: messaging.BaseAdapter[*YuanxinConn]{
				PlatformAdapter: messaging.PlatformAdapter{Log: log.With("channel", string(messaging.PlatformYuanxin))},
			},
		}
	})
}

type Adapter struct {
	messaging.BaseAdapter[*YuanxinConn]

	mu            sync.RWMutex
	appID         string
	pulsarURL     string
	tenant        string
	ns            string
	producerTopic string
	client        pulsar.Client
	consumer      pulsar.Consumer
	producer      pulsar.Producer

	cancelFunc   context.CancelFunc
	consumerDone chan struct{}
}

func (a *Adapter) Platform() messaging.PlatformType { return messaging.PlatformYuanxin }

var _ messaging.PlatformAdapterInterface = (*Adapter)(nil)
var _ messaging.CronResultSender = (*Adapter)(nil)

func (a *Adapter) GetBotID() string { return a.appID }

// ConfigureWith initializes adapter fields. Must be called before Start;
// after Start returns, these fields are read-only and accessed without locks.
func (a *Adapter) ConfigureWith(config messaging.AdapterConfig) error {
	_ = a.PlatformAdapter.ConfigureWith(config)
	a.ConfigureShared(config)

	a.appID = config.ExtrasString("app_id")
	a.pulsarURL = config.ExtrasString("pulsar_url")
	if a.pulsarURL == "" {
		a.pulsarURL = "pulsar://localhost:6650"
	}
	if !strings.HasPrefix(a.pulsarURL, "pulsar://") && !strings.HasPrefix(a.pulsarURL, "pulsar+ssl://") {
		return fmt.Errorf("yuanxin: pulsar_url must start with pulsar:// or pulsar+ssl://")
	}
	a.tenant = config.ExtrasString("tenant")
	if a.tenant == "" {
		a.tenant = "public"
	}
	a.ns = config.ExtrasString("namespace")
	if a.ns == "" {
		a.ns = "default"
	}
	a.producerTopic = config.ExtrasString("producer_topic")
	if a.producerTopic == "" {
		a.producerTopic = "global-open-claw-response-topic"
	}

	if a.appID == "" {
		return fmt.Errorf("yuanxin: app_id is required")
	}
	return nil
}

func (a *Adapter) Start(ctx context.Context) error {
	if !a.StartGuard() {
		a.Log.Warn("yuanxin: adapter already started, skipping")
		return nil
	}

	a.InitSharedState()
	a.InitConnPool(func(key string) *YuanxinConn {
		parts := strings.SplitN(key, "#", 2)
		channelID := parts[0]
		threadKey := ""
		if len(parts) > 1 {
			threadKey = parts[1]
		}
		return NewYuanxinConn(a, channelID, threadKey, a.Bridge().WorkDir())
	})

	consumerCtx, cancel := context.WithCancel(ctx)
	a.cancelFunc = cancel
	go a.runConsumer(consumerCtx)
	return nil
}

func (a *Adapter) runConsumer(ctx context.Context) {
	defer func() {
		if r := recover(); r != nil {
			a.Log.Error("yuanxin: panic in consumer", "panic", r)
		}
	}()

	baseDelay := a.BackoffBaseDelay
	if baseDelay <= 0 {
		baseDelay = 2 * time.Second
	}
	maxDelay := a.BackoffMaxDelay
	if maxDelay <= 0 {
		maxDelay = 60 * time.Second
	}
	backoff := messaging.NewReconnectBackoff(baseDelay, maxDelay)

	attempt := 1
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		if err := a.connect(); err != nil {
			a.Log.Warn("yuanxin: connection failed, reconnecting", "err", err, "attempt", attempt)
			backoffDelay := backoff.Next()
			select {
			case <-ctx.Done():
				return
			case <-time.After(backoffDelay):
			}
			attempt++
			continue
		}

		backoff.Reset()
		attempt = 1

		consumer, producer := a.getConnResources()
		if consumer == nil || producer == nil {
			continue
		}

		done := make(chan struct{})
		a.mu.Lock()
		a.consumerDone = done
		a.mu.Unlock()

		go a.consumeLoop(ctx, consumer, done)

		select {
		case <-ctx.Done():
			a.mu.Lock()
			a.consumerDone = nil
			a.mu.Unlock()
			return
		case <-done:
			a.Log.Warn("yuanxin: consume loop exited, reconnecting", "attempt", attempt)
			a.mu.Lock()
			a.consumerDone = nil
			a.mu.Unlock()
			a.cleanupConn()
		}
	}
}

func (a *Adapter) getConnResources() (pulsar.Consumer, pulsar.Producer) {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.consumer, a.producer
}

func (a *Adapter) cleanupConn() {
	a.mu.Lock()
	consumer := a.consumer
	producer := a.producer
	client := a.client
	a.consumer = nil
	a.producer = nil
	a.client = nil
	a.mu.Unlock()

	if consumer != nil {
		consumer.Close()
	}
	if producer != nil {
		producer.Close()
	}
	if client != nil {
		client.Close()
	}
}

func (a *Adapter) connect() error {
	a.mu.Lock()
	if a.consumer != nil && a.producer != nil {
		a.mu.Unlock()
		return nil
	}
	var oldClient pulsar.Client
	if a.client != nil {
		oldClient = a.client
		a.client = nil
		a.consumer = nil
		a.producer = nil
	}
	a.mu.Unlock()

	if oldClient != nil {
		oldClient.Close()
	}

	client, err := pulsar.NewClient(pulsar.ClientOptions{
		URL: a.pulsarURL,
	})
	if err != nil {
		return fmt.Errorf("yuanxin: create client: %w", err)
	}

	consumerTopic := fmt.Sprintf("persistent://%s/%s/chatbot-%s-request", a.tenant, a.ns, a.appID)
	consumer, err := client.Subscribe(pulsar.ConsumerOptions{
		Topic:            consumerTopic,
		SubscriptionName: fmt.Sprintf("hotplex-gateway-%s", a.appID),
		Type:             pulsar.Shared,
	})
	if err != nil {
		client.Close()
		return fmt.Errorf("yuanxin: subscribe: %w", err)
	}

	producerTopic := ""
	if strings.Contains(a.producerTopic, "://") {
		producerTopic = a.producerTopic
	} else {
		producerTopic = fmt.Sprintf("persistent://%s/%s/%s", a.tenant, a.ns, a.producerTopic)
	}
	producer, err := client.CreateProducer(pulsar.ProducerOptions{
		Topic: producerTopic,
	})
	if err != nil {
		consumer.Close()
		client.Close()
		return fmt.Errorf("yuanxin: create producer: %w", err)
	}

	a.mu.Lock()
	if a.consumer != nil && a.producer != nil {
		a.mu.Unlock()
		producer.Close()
		consumer.Close()
		client.Close()
		a.Log.Debug("yuanxin: connect: another connect() completed first, discarding duplicate resources")
		return nil
	}
	a.client = client
	a.consumer = consumer
	a.producer = producer
	a.mu.Unlock()

	a.Log.Info("yuanxin: connected", "consumer_topic", consumerTopic, "producer_topic", producerTopic)

	return nil
}

func (a *Adapter) consumeLoop(ctx context.Context, consumer pulsar.Consumer, done chan struct{}) {
	defer close(done)

	const maxReceiveRetries = 3
	receiveRetries := 0

	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		msg, err := consumer.Receive(ctx)
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			receiveRetries++
			if receiveRetries < maxReceiveRetries {
				a.Log.Warn("yuanxin: receive error, retrying", "err", err, "attempt", receiveRetries)
				continue
			}
			a.Log.Warn("yuanxin: receive error, max retries exceeded, exiting consume loop", "err", err, "attempts", receiveRetries)
			return
		}
		receiveRetries = 0

		a.Log.Debug("yuanxin: message received from Pulsar", "msg_id", msg.ID().String(), "payload_len", len(msg.Payload()))

		if err := a.handleMessage(ctx, msg); err != nil {
			a.Log.Error("yuanxin: handle message error", "err", err)
			consumer.Nack(msg)
			continue
		}

		if err := consumer.Ack(msg); err != nil {
			a.Log.Error("yuanxin: ack message error", "err", err)
		}
	}
}

type YuanxinMessage struct {
	Metadata map[string]any `json:"metadata"`
	Msg      string         `json:"msg"`
}

func (a *Adapter) handleMessage(ctx context.Context, msg pulsar.Message) error {
	a.Log.Debug("yuanxin: handleMessage called", "msg_id", msg.ID().String())

	var yuanxinMsg YuanxinMessage
	if err := json.Unmarshal(msg.Payload(), &yuanxinMsg); err != nil {
		a.Log.Error("yuanxin: unmarshal failed", "err", err, "payload", string(msg.Payload()))
		return fmt.Errorf("yuanxin: unmarshal: %w", err)
	}

	a.Log.Debug("yuanxin: message parsed", "metadata", yuanxinMsg.Metadata, "msg", yuanxinMsg.Msg)

	platformMsgID := msg.ID().String()
	text := messaging.SanitizeText(yuanxinMsg.Msg)
	if text == "" {
		a.Log.Warn("yuanxin: empty text after sanitize, skipping")
		return nil
	}

	if a.Dedup != nil && !a.Dedup.TryRecord(platformMsgID) {
		a.Log.Warn("yuanxin: duplicate message, skipping", "msg_id", platformMsgID)
		return nil
	}

	userID := metadataString(yuanxinMsg.Metadata, "replyUserCodes")
	if userID == "" {
		userID = platformMsgID
	}
	channelID := userID
	if channelID == "" {
		channelID = platformMsgID
	}

	if a.Gate != nil {
		allowed, reason := a.Gate.Check(true, userID, false)
		a.Log.Debug("yuanxin: gate check", "allowed", allowed, "reason", reason, "user", userID)
		if !allowed {
			return nil
		}
	}

	conn := a.GetOrCreateConn(userID, channelID)

	if len(yuanxinMsg.Metadata) > 0 {
		conn.SetMetadata(yuanxinMsg.Metadata)
	}

	envelope := a.Bridge().MakeEnvelope(userID, text, session.PlatformContext{
		Platform:  string(messaging.PlatformYuanxin),
		BotID:     a.appID,
		ChannelID: channelID,
		ThreadTS:  channelID,
		UserID:    userID,
		WorkDir:   a.Bridge().WorkDir(),
	})
	if envelope == nil {
		return fmt.Errorf("yuanxin: failed to build envelope")
	}

	if md, ok := envelope.Event.Data.(map[string]any); ok {
		md["platform_msg_id"] = platformMsgID
	}

	a.Log.Debug("yuanxin: calling Bridge.Handle", "session_id", envelope.SessionID, "owner_id", envelope.OwnerID, "text", text)

	if err := a.Bridge().Handle(ctx, envelope, conn); err != nil {
		a.Log.Error("yuanxin: Bridge.Handle failed", "err", err)
		return err
	}
	a.Log.Debug("yuanxin: Bridge.Handle succeeded", "session_id", envelope.SessionID)
	return nil
}

func (a *Adapter) HandleTextMessage(_ context.Context, _, _, _, _, _, _ string) error {
	return nil
}

func (a *Adapter) SendCronResult(ctx context.Context, text string, platformKey map[string]string) error {
	messageID := platformKey["message_id"]
	if messageID == "" {
		return fmt.Errorf("yuanxin: missing message_id in platform_key")
	}
	text = messaging.SanitizeText(text)

	md := map[string]any{
		"botId":          a.appID,
		"messageId":      messageID,
		"replyUserCodes": platformKey["reply_user_codes"],
		"sysId":          platformKey["sys_id"],
		"platform":       "yx",
	}
	if secret, ok := platformKey["secret"]; ok {
		md["secret"] = secret
	}

	response := YuanxinMessage{
		Metadata: md,
		Msg:      text,
	}

	data, err := json.Marshal(response)
	if err != nil {
		return fmt.Errorf("yuanxin: marshal cron result: %w", err)
	}

	a.mu.RLock()
	producer := a.producer
	a.mu.RUnlock()
	if producer == nil {
		return fmt.Errorf("yuanxin: producer not initialized")
	}

	_, err = producer.Send(ctx, &pulsar.ProducerMessage{Payload: data})
	if err != nil {
		return fmt.Errorf("yuanxin: send cron result: %w", err)
	}
	return nil
}

func metadataString(md map[string]any, key string) string {
	if md == nil {
		return ""
	}
	v, ok := md[key]
	if !ok {
		return ""
	}
	s, ok := v.(string)
	if !ok {
		return ""
	}
	return s
}

func (a *Adapter) Close(ctx context.Context) error {
	a.Log.Info("yuanxin: adapter closing")

	a.mu.Lock()
	cancel := a.cancelFunc
	done := a.consumerDone
	a.mu.Unlock()

	if cancel != nil {
		cancel()
	}

	if done != nil {
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			a.Log.Warn("yuanxin: timed out waiting for consumer loop to exit")
		}
	}

	a.MarkClosed()

	a.cleanupConn()

	a.CloseSharedState()
	conns := a.DrainConns()
	for _, conn := range conns {
		_ = conn.Close()
	}

	return nil
}

func (a *Adapter) SendResponse(ctx context.Context, conn *YuanxinConn, content string) error {
	a.mu.RLock()
	producer := a.producer
	a.mu.RUnlock()

	if producer == nil {
		return fmt.Errorf("yuanxin: producer not initialized")
	}

	response := YuanxinMessage{
		Metadata: conn.GetMetadata(),
		Msg:      content,
	}

	data, err := json.Marshal(response)
	if err != nil {
		return fmt.Errorf("yuanxin: marshal response: %w", err)
	}

	a.Log.Debug("yuanxin: sending response to pulsar",
		"content_len", len(content),
		"metadata_keys", len(response.Metadata))

	_, err = producer.Send(ctx, &pulsar.ProducerMessage{
		Payload: data,
	})
	if err != nil {
		a.Log.Error("yuanxin: failed to send response to pulsar", "err", err)
	} else {
		a.Log.Debug("yuanxin: response sent to pulsar successfully")
	}
	return err
}

const maxTextBuilderSize = 1 << 20

type YuanxinConn struct {
	adapter     *Adapter
	channelID   string
	threadKey   string
	workDir     string
	metadata    map[string]any
	textBuilder strings.Builder
	mu          sync.RWMutex
}

func NewYuanxinConn(adapter *Adapter, channelID, threadKey, workDir string) *YuanxinConn {
	return &YuanxinConn{
		adapter:   adapter,
		channelID: channelID,
		threadKey: threadKey,
		workDir:   workDir,
		metadata:  make(map[string]any),
	}
}

func (c *YuanxinConn) SetMetadata(md map[string]any) {
	if md == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.metadata = make(map[string]any, len(md))
	for k, v := range md {
		c.metadata[k] = v
	}
}

func (c *YuanxinConn) GetMetadata() map[string]any {
	c.mu.RLock()
	defer c.mu.RUnlock()
	result := make(map[string]any, len(c.metadata))
	for k, v := range c.metadata {
		result[k] = v
	}
	return result
}

func (c *YuanxinConn) WriteCtx(ctx context.Context, env *events.Envelope) error {
	if env == nil {
		return fmt.Errorf("yuanxin: nil envelope")
	}

	c.adapter.Log.Debug("yuanxin: WriteCtx called",
		"event_type", env.Event.Type,
		"session_id", env.SessionID,
		"seq", env.Seq)

	switch env.Event.Type {
	case events.Done:
		c.adapter.Interactions.CancelAll(env.SessionID)
		c.mu.Lock()
		text := c.textBuilder.String()
		c.textBuilder.Reset()
		c.mu.Unlock()
		if text == "" {
			return nil
		}
		return c.adapter.SendResponse(ctx, c, text)
	case events.Error:
		c.adapter.Interactions.CancelAll(env.SessionID)
		c.mu.Lock()
		c.textBuilder.Reset()
		c.mu.Unlock()
		if errMsg := messaging.ExtractErrorMessage(env); errMsg != "" {
			return c.adapter.SendResponse(ctx, c, errMsg)
		}
		return nil
	case events.PermissionRequest:
		return c.sendPermissionRequest(ctx, env)
	case events.QuestionRequest:
		return c.sendQuestionRequest(ctx, env)
	case events.ElicitationRequest:
		return c.sendElicitationRequest(ctx, env)
	case events.ContextUsage:
		return c.sendContextUsage(ctx, env)
	case events.MessageDelta:
		if d, ok := env.Event.Data.(events.MessageDeltaData); ok && d.Content != "" {
			c.mu.Lock()
			if c.textBuilder.Len() < maxTextBuilderSize {
				c.textBuilder.WriteString(d.Content)
			}
			c.mu.Unlock()
		}
		return nil
	}

	text, ok := messaging.ExtractResponseText(env)
	if !ok || text == "" {
		c.adapter.Log.Debug("yuanxin: WriteCtx no text extracted",
			"event_type", env.Event.Type,
			"ok", ok)
		return nil
	}

	c.adapter.Log.Debug("yuanxin: WriteCtx sending response",
		"event_type", env.Event.Type,
		"text_len", len(text))

	return c.adapter.SendResponse(ctx, c, text)
}

func (c *YuanxinConn) Close() error {
	c.adapter.DeleteConn(c.channelID, c.threadKey)
	return nil
}

func (c *YuanxinConn) sendPermissionRequest(ctx context.Context, env *events.Envelope) error {
	d, err := messaging.ExtractPermissionData(env)
	if err != nil {
		c.adapter.Log.Warn("yuanxin: extract permission data failed", "err", err, "session_id", env.SessionID)
		return nil
	}

	text := fmt.Sprintf("权限请求：%s\n\n说明：%s\n\n选项：允许(allow)/拒绝(deny)", d.ToolName, d.Description)
	return c.adapter.SendResponse(ctx, c, text)
}

func (c *YuanxinConn) sendQuestionRequest(ctx context.Context, env *events.Envelope) error {
	d, err := messaging.ExtractQuestionData(env)
	if err != nil {
		c.adapter.Log.Warn("yuanxin: extract question data failed", "err", err, "session_id", env.SessionID)
		return nil
	}

	questions := make([]string, 0, len(d.Questions))
	for _, q := range d.Questions {
		questions = append(questions, q.Question)
	}
	text := fmt.Sprintf("问题：%s\n\n选项：%s", strings.Join(questions, ", "), d.ToolName)
	return c.adapter.SendResponse(ctx, c, text)
}

func (c *YuanxinConn) sendElicitationRequest(ctx context.Context, env *events.Envelope) error {
	d, err := messaging.ExtractElicitationData(env)
	if err != nil {
		c.adapter.Log.Warn("yuanxin: extract elicitation data failed", "err", err, "session_id", env.SessionID)
		return nil
	}

	text := fmt.Sprintf("MCP 选择：%s\n\n说明：%s", d.MCPServerName, d.Message)
	return c.adapter.SendResponse(ctx, c, text)
}

func (c *YuanxinConn) sendContextUsage(ctx context.Context, env *events.Envelope) error {
	d, err := messaging.ExtractContextUsageData(env)
	if err != nil {
		c.adapter.Log.Warn("yuanxin: extract context usage data failed", "err", err, "session_id", env.SessionID)
		return nil
	}

	text := messaging.FormatCanonicalText(d)
	return c.adapter.SendResponse(ctx, c, text)
}

var _ messaging.PlatformConn = (*YuanxinConn)(nil)
