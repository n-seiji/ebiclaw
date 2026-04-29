package slack

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"github.com/slack-go/slack"
	"github.com/slack-go/slack/slackevents"
	"github.com/slack-go/slack/socketmode"

	"github.com/sipeed/picoclaw/pkg/bus"
	"github.com/sipeed/picoclaw/pkg/channels"
	"github.com/sipeed/picoclaw/pkg/config"
	"github.com/sipeed/picoclaw/pkg/identity"
	"github.com/sipeed/picoclaw/pkg/logger"
	"github.com/sipeed/picoclaw/pkg/media"
	"github.com/sipeed/picoclaw/pkg/utils"
)

type SlackChannel struct {
	*channels.BaseChannel
	config       config.SlackConfig
	api          *slack.Client
	socketClient *socketmode.Client
	botUserID    string
	teamID       string
	ctx          context.Context
	cancel       context.CancelFunc
	pendingAcks  sync.Map
	// channelMetaCache memoises slack conversation lookups so repeated messages
	// on the same channel do not hammer the conversations.info endpoint.
	channelMetaCache sync.Map // map[channelID]channelMeta
}

type channelMeta struct {
	name      string
	isPrivate bool
	isIM      bool
}

type slackMessageRef struct {
	ChannelID string
	Timestamp string
}

func NewSlackChannel(cfg config.SlackConfig, messageBus *bus.MessageBus) (*SlackChannel, error) {
	if cfg.BotToken.String() == "" || cfg.AppToken.String() == "" {
		return nil, fmt.Errorf("slack bot_token and app_token are required")
	}

	api := slack.New(
		cfg.BotToken.String(),
		slack.OptionAppLevelToken(cfg.AppToken.String()),
	)

	socketClient := socketmode.New(api)

	base := channels.NewBaseChannel("slack", cfg, messageBus, cfg.AllowFrom,
		channels.WithMaxMessageLength(40000),
		channels.WithGroupTrigger(cfg.GroupTrigger),
		channels.WithReasoningChannelID(cfg.ReasoningChannelID),
	)

	return &SlackChannel{
		BaseChannel:  base,
		config:       cfg,
		api:          api,
		socketClient: socketClient,
	}, nil
}

func (c *SlackChannel) Start(ctx context.Context) error {
	logger.InfoC("slack", "Starting Slack channel (Socket Mode)")

	c.ctx, c.cancel = context.WithCancel(ctx)

	authResp, err := c.api.AuthTest()
	if err != nil {
		return fmt.Errorf("slack auth test failed: %w", err)
	}
	c.botUserID = authResp.UserID
	c.teamID = authResp.TeamID

	logger.InfoCF("slack", "Slack bot connected", map[string]any{
		"bot_user_id": c.botUserID,
		"team":        authResp.Team,
	})

	go c.eventLoop()

	go func() {
		if err := c.socketClient.RunContext(c.ctx); err != nil {
			if c.ctx.Err() == nil {
				logger.ErrorCF("slack", "Socket Mode connection error", map[string]any{
					"error": err.Error(),
				})
			}
		}
	}()

	c.SetRunning(true)
	logger.InfoC("slack", "Slack channel started (Socket Mode)")
	return nil
}

func (c *SlackChannel) Stop(ctx context.Context) error {
	logger.InfoC("slack", "Stopping Slack channel")

	if c.cancel != nil {
		c.cancel()
	}

	c.SetRunning(false)
	logger.InfoC("slack", "Slack channel stopped")
	return nil
}

func (c *SlackChannel) Send(ctx context.Context, msg bus.OutboundMessage) ([]string, error) {
	if !c.IsRunning() {
		return nil, channels.ErrNotRunning
	}

	channelID, threadTS := parseSlackChatID(msg.ChatID)
	if channelID == "" {
		return nil, fmt.Errorf("invalid slack chat ID: %s", msg.ChatID)
	}

	opts := []slack.MsgOption{
		slack.MsgOptionText(msg.Content, false),
	}

	if msg.ReplyToMessageID != "" && threadTS == "" {
		// Answer to the message by creating a Thread under it
		opts = append(opts, slack.MsgOptionTS(msg.ReplyToMessageID))
	} else if threadTS != "" {
		// If we are already in a thread, continue in the thread
		opts = append(opts, slack.MsgOptionTS(threadTS))
	}

	_, ts, err := c.api.PostMessageContext(ctx, channelID, opts...)
	if err != nil {
		return nil, fmt.Errorf("slack send: %w", channels.ErrTemporary)
	}

	if ref, ok := c.pendingAcks.LoadAndDelete(msg.ChatID); ok {
		msgRef := ref.(slackMessageRef)
		c.api.AddReaction("white_check_mark", slack.ItemRef{
			Channel:   msgRef.ChannelID,
			Timestamp: msgRef.Timestamp,
		})
	}

	logger.DebugCF("slack", "Message sent", map[string]any{
		"channel_id": channelID,
		"thread_ts":  threadTS,
	})

	return []string{ts}, nil
}

// SendMedia implements the channels.MediaSender interface.
func (c *SlackChannel) SendMedia(ctx context.Context, msg bus.OutboundMediaMessage) ([]string, error) {
	if !c.IsRunning() {
		return nil, channels.ErrNotRunning
	}

	channelID, _ := parseSlackChatID(msg.ChatID)
	if channelID == "" {
		return nil, fmt.Errorf("invalid slack chat ID: %s", msg.ChatID)
	}

	store := c.GetMediaStore()
	if store == nil {
		return nil, fmt.Errorf("no media store available: %w", channels.ErrSendFailed)
	}

	for _, part := range msg.Parts {
		localPath, err := store.Resolve(part.Ref)
		if err != nil {
			logger.ErrorCF("slack", "Failed to resolve media ref", map[string]any{
				"ref":   part.Ref,
				"error": err.Error(),
			})
			continue
		}

		filename := part.Filename
		if filename == "" {
			filename = "file"
		}

		title := part.Caption
		if title == "" {
			title = filename
		}

		_, err = c.api.UploadFileV2Context(ctx, slack.UploadFileV2Parameters{
			Channel:  channelID,
			File:     localPath,
			Filename: filename,
			Title:    title,
		})
		if err != nil {
			logger.ErrorCF("slack", "Failed to upload media", map[string]any{
				"filename": filename,
				"error":    err.Error(),
			})
			return nil, fmt.Errorf("slack send media: %w", channels.ErrTemporary)
		}
	}

	// UploadFileV2 does not expose the posted message timestamp in its
	// response; returning nil avoids conflating file IDs with message IDs.
	return nil, nil
}

// ReactToMessage implements channels.ReactionCapable.
// It adds an "eyes" (👀) reaction to the inbound message and returns an undo function
// that removes the reaction.
func (c *SlackChannel) ReactToMessage(ctx context.Context, chatID, messageID string) (func(), error) {
	channelID, _ := parseSlackChatID(chatID)
	if channelID == "" {
		return func() {}, nil
	}

	c.api.AddReaction("eyes", slack.ItemRef{
		Channel:   channelID,
		Timestamp: messageID,
	})

	return func() {
		c.api.RemoveReaction("eyes", slack.ItemRef{
			Channel:   channelID,
			Timestamp: messageID,
		})
	}, nil
}

func (c *SlackChannel) eventLoop() {
	for {
		select {
		case <-c.ctx.Done():
			return
		case event, ok := <-c.socketClient.Events:
			if !ok {
				return
			}
			switch event.Type {
			case socketmode.EventTypeEventsAPI:
				c.handleEventsAPI(event)
			case socketmode.EventTypeSlashCommand:
				c.handleSlashCommand(event)
			case socketmode.EventTypeInteractive:
				if event.Request != nil {
					c.socketClient.Ack(*event.Request)
				}
			}
		}
	}
}

func (c *SlackChannel) handleEventsAPI(event socketmode.Event) {
	if event.Request != nil {
		c.socketClient.Ack(*event.Request)
	}

	eventsAPIEvent, ok := event.Data.(slackevents.EventsAPIEvent)
	if !ok {
		return
	}

	switch ev := eventsAPIEvent.InnerEvent.Data.(type) {
	case *slackevents.MessageEvent:
		c.handleMessageEvent(ev)
	case *slackevents.AppMentionEvent:
		c.handleAppMention(ev)
	}
}

func (c *SlackChannel) handleMessageEvent(ev *slackevents.MessageEvent) {
	if ev.User == c.botUserID || ev.User == "" {
		return
	}
	if ev.BotID != "" {
		return
	}
	if ev.SubType != "" && ev.SubType != "file_share" {
		return
	}
	if c.shouldIgnoreChannel(ev.Channel) {
		logger.DebugCF("slack", "Message dropped by channel filter", map[string]any{
			"channel_id": ev.Channel,
		})
		return
	}

	// check allowlist to avoid downloading attachments for rejected users
	sender := bus.SenderInfo{
		Platform:    "slack",
		PlatformID:  ev.User,
		CanonicalID: identity.BuildCanonicalID("slack", ev.User),
	}
	if !c.IsAllowedSender(sender) {
		logger.DebugCF("slack", "Message rejected by allowlist", map[string]any{
			"user_id": ev.User,
		})
		return
	}

	senderID := ev.User
	channelID := ev.Channel
	threadTS := ev.ThreadTimeStamp
	messageTS := ev.TimeStamp

	chatID := channelID
	if threadTS != "" {
		chatID = channelID + "/" + threadTS
	}

	c.pendingAcks.Store(chatID, slackMessageRef{
		ChannelID: channelID,
		Timestamp: messageTS,
	})

	content := ev.Text
	content = c.stripBotMention(content)

	// In non-DM channels, apply group trigger filtering
	if !strings.HasPrefix(channelID, "D") {
		respond, cleaned := c.ShouldRespondInGroup(false, content)
		if !respond {
			return
		}
		content = cleaned
	}

	var mediaPaths []string

	scope := channels.BuildMediaScope("slack", chatID, messageTS)

	// Helper to register a local file with the media store
	storeMedia := func(localPath, filename string) string {
		if store := c.GetMediaStore(); store != nil {
			ref, err := store.Store(localPath, media.MediaMeta{
				Filename:      filename,
				Source:        "slack",
				CleanupPolicy: media.CleanupPolicyDeleteOnCleanup,
			}, scope)
			if err == nil {
				return ref
			}
		}
		return localPath // fallback
	}

	if ev.Message != nil && len(ev.Message.Files) > 0 {
		for _, file := range ev.Message.Files {
			localPath := c.downloadSlackFile(file)
			if localPath == "" {
				continue
			}
			mediaPaths = append(mediaPaths, storeMedia(localPath, file.Name))
			content += fmt.Sprintf("\n[file: %s]", file.Name)
		}
	}

	if strings.TrimSpace(content) == "" {
		return
	}

	peerKind := "channel"
	peerID := channelID
	if strings.HasPrefix(channelID, "D") {
		peerKind = "direct"
		peerID = senderID
	} else if threadTS != "" && c.isPerThreadChannel(channelID) {
		// Channel name opted in to per-thread session scoping by suffix.
		peerID = channelID + "/" + threadTS
	}

	peer := bus.Peer{Kind: peerKind, ID: peerID}

	metadata := map[string]string{
		"message_ts": messageTS,
		"channel_id": channelID,
		"thread_ts":  threadTS,
		"platform":   "slack",
		"team_id":    c.teamID,
	}

	logger.DebugCF("slack", "Received message", map[string]any{
		"sender_id":  senderID,
		"chat_id":    chatID,
		"preview":    utils.Truncate(content, 50),
		"has_thread": threadTS != "",
	})

	c.HandleMessage(c.ctx, peer, messageTS, senderID, chatID, content, mediaPaths, metadata, sender)
}

func (c *SlackChannel) handleAppMention(ev *slackevents.AppMentionEvent) {
	if ev.User == c.botUserID {
		return
	}
	if c.shouldIgnoreChannel(ev.Channel) {
		logger.DebugCF("slack", "Mention dropped by channel filter", map[string]any{
			"channel_id": ev.Channel,
		})
		return
	}

	if !c.IsAllowedSender(bus.SenderInfo{
		Platform:    "slack",
		PlatformID:  ev.User,
		CanonicalID: identity.BuildCanonicalID("slack", ev.User),
	}) {
		logger.DebugCF("slack", "Mention rejected by allowlist", map[string]any{
			"user_id": ev.User,
		})
		return
	}

	senderID := ev.User
	mentionSender := bus.SenderInfo{
		Platform:    "slack",
		PlatformID:  senderID,
		CanonicalID: identity.BuildCanonicalID("slack", senderID),
	}
	channelID := ev.Channel
	threadTS := ev.ThreadTimeStamp
	messageTS := ev.TimeStamp

	var chatID string
	if threadTS != "" {
		chatID = channelID + "/" + threadTS
	} else {
		chatID = channelID + "/" + messageTS
	}

	c.pendingAcks.Store(chatID, slackMessageRef{
		ChannelID: channelID,
		Timestamp: messageTS,
	})

	content := c.stripBotMention(ev.Text)

	if strings.TrimSpace(content) == "" {
		return
	}

	mentionPeerKind := "channel"
	mentionPeerID := channelID
	if strings.HasPrefix(channelID, "D") {
		mentionPeerKind = "direct"
		mentionPeerID = senderID
	} else if threadTS != "" && c.isPerThreadChannel(channelID) {
		// Channel name opted in to per-thread session scoping by suffix.
		mentionPeerID = channelID + "/" + threadTS
	}

	mentionPeer := bus.Peer{Kind: mentionPeerKind, ID: mentionPeerID}

	metadata := map[string]string{
		"message_ts": messageTS,
		"channel_id": channelID,
		"thread_ts":  threadTS,
		"platform":   "slack",
		"is_mention": "true",
		"team_id":    c.teamID,
	}

	c.HandleMessage(c.ctx, mentionPeer, messageTS, senderID, chatID, content, nil, metadata, mentionSender)
}

func (c *SlackChannel) handleSlashCommand(event socketmode.Event) {
	cmd, ok := event.Data.(slack.SlashCommand)
	if !ok {
		return
	}

	if event.Request != nil {
		c.socketClient.Ack(*event.Request)
	}

	cmdSender := bus.SenderInfo{
		Platform:    "slack",
		PlatformID:  cmd.UserID,
		CanonicalID: identity.BuildCanonicalID("slack", cmd.UserID),
	}
	if !c.IsAllowedSender(cmdSender) {
		logger.DebugCF("slack", "Slash command rejected by allowlist", map[string]any{
			"user_id": cmd.UserID,
		})
		return
	}

	senderID := cmd.UserID
	channelID := cmd.ChannelID
	chatID := channelID
	content := cmd.Text

	if strings.TrimSpace(content) == "" {
		content = "help"
	}

	metadata := map[string]string{
		"channel_id": channelID,
		"platform":   "slack",
		"is_command": "true",
		"trigger_id": cmd.TriggerID,
		"team_id":    c.teamID,
	}

	logger.DebugCF("slack", "Slash command received", map[string]any{
		"sender_id": senderID,
		"command":   cmd.Command,
		"text":      utils.Truncate(content, 50),
	})

	c.HandleMessage(
		c.ctx,
		bus.Peer{Kind: "channel", ID: channelID},
		"",
		senderID,
		chatID,
		content,
		nil,
		metadata,
		cmdSender,
	)
}

func (c *SlackChannel) downloadSlackFile(file slack.File) string {
	downloadURL := file.URLPrivateDownload
	if downloadURL == "" {
		downloadURL = file.URLPrivate
	}
	if downloadURL == "" {
		logger.ErrorCF("slack", "No download URL for file", map[string]any{"file_id": file.ID})
		return ""
	}

	return utils.DownloadFile(downloadURL, file.Name, utils.DownloadOptions{
		LoggerPrefix: "slack",
		ExtraHeaders: map[string]string{
			"Authorization": "Bearer " + c.config.BotToken.String(),
		},
	})
}

func (c *SlackChannel) stripBotMention(text string) string {
	mention := fmt.Sprintf("<@%s>", c.botUserID)
	text = strings.ReplaceAll(text, mention, "")
	return strings.TrimSpace(text)
}

// shouldIgnoreChannel reports whether messages from the given channel should
// be dropped before any agent processing. Direct messages always pass; private
// group channels and channels whose names start with "ext-" or "ext_" are
// ignored. The per-channel result is cached in channelMetaCache.
func (c *SlackChannel) shouldIgnoreChannel(channelID string) bool {
	if channelID == "" {
		return false
	}
	// DM channels (ID prefix "D") are always allowed without an API lookup.
	if strings.HasPrefix(channelID, "D") {
		return false
	}

	if cached, ok := c.channelMetaCache.Load(channelID); ok {
		meta := cached.(channelMeta)
		return ignoreByMeta(meta)
	}

	info, err := c.api.GetConversationInfoContext(c.ctx, &slack.GetConversationInfoInput{
		ChannelID: channelID,
	})
	if err != nil {
		// If we cannot resolve, fail open (allow) so transient API errors do
		// not silently drop user messages. The lookup will be retried next
		// time because we do not cache the failure.
		logger.WarnCF("slack", "Failed to look up conversation info; allowing message", map[string]any{
			"channel_id": channelID,
			"error":      err.Error(),
		})
		return false
	}

	meta := channelMeta{
		name:      info.Name,
		isPrivate: info.IsPrivate,
		isIM:      info.IsIM,
	}
	c.channelMetaCache.Store(channelID, meta)
	return ignoreByMeta(meta)
}

// ignoreByMeta encapsulates the policy: direct messages are never ignored,
// private group channels and ext- / ext_ prefixed channels are ignored.
func ignoreByMeta(m channelMeta) bool {
	if m.isIM {
		return false
	}
	if m.isPrivate {
		return true
	}
	name := strings.ToLower(m.name)
	if strings.HasPrefix(name, "ext-") || strings.HasPrefix(name, "ext_") {
		return true
	}
	return false
}

// isPerThreadChannel checks the channel name suffix to decide whether the
// session should be scoped per thread. shouldIgnoreChannel must have been
// called first so the cache entry exists; if not we fall back to channel-wide.
func (c *SlackChannel) isPerThreadChannel(channelID string) bool {
	if channelID == "" {
		return false
	}
	cached, ok := c.channelMetaCache.Load(channelID)
	if !ok {
		return false
	}
	meta := cached.(channelMeta)
	name := strings.ToLower(meta.name)
	return strings.HasSuffix(name, "_per_thread") || strings.HasSuffix(name, "-per-thread")
}

func parseSlackChatID(chatID string) (channelID, threadTS string) {
	parts := strings.SplitN(chatID, "/", 2)
	channelID = parts[0]
	if len(parts) > 1 {
		threadTS = parts[1]
	}
	return channelID, threadTS
}
