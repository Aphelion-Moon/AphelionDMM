package server

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	collabstore "sdmm/internal/aphelion/collab/store"
)

const discordStatusInterval = 30 * time.Second

type HostedDiscordNotifications struct {
	GuildID   string       `yaml:"guild_id"`
	ChannelID string       `yaml:"channel_id"`
	BotToken  SecretSource `yaml:"bot_token"`
}

// RunDiscordNotifications is a best-effort observer. Discord failures never gate
// session creation, admission or durable edits. The caller owns its context.
func (service *Service) RunDiscordNotifications(ctx context.Context, config HostedDiscordNotifications, token string, reportError func(error)) {
	registry, supported := service.config.HostedRegistry.(collabstore.HostedNotificationStore)
	if !supported {
		if reportError != nil {
			reportError(fmt.Errorf("session registry does not support bounded notification summaries"))
		}
		return
	}
	reporter := discordSessionReporter{
		client:  &http.Client{Timeout: 10 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }},
		baseURL: "https://discord.com/api/v10", guildID: config.GuildID, channelID: config.ChannelID, token: token,
	}
	for ctx.Err() == nil {
		delay := discordStatusInterval
		pollContext, cancel := context.WithTimeout(ctx, 15*time.Second)
		counts := service.hostedActivity()
		activeIDs := make([]string, 0, len(counts))
		for id := range counts {
			activeIDs = append(activeIDs, id)
		}
		summary, err := registry.HostedNotificationSummary(pollContext, activeIDs)
		if err == nil {
			delay, err = reporter.publish(pollContext, discordSessionStatus(summary, counts))
		} else {
			err = fmt.Errorf("discord session status could not read the session registry")
		}
		cancel()
		if err != nil && ctx.Err() == nil && reportError != nil {
			reportError(err)
		}
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
	}
}

func discordSessionStatus(summary collabstore.HostedNotificationSummary, counts map[string]int) string {
	var text strings.Builder
	fmt.Fprintf(&text, "AphelionDMM hosted sessions\nCommunity sessions: %d\n", summary.CommunityCount)
	escape := strings.NewReplacer("\\", "\\\\", "`", "\\`", "*", "\\*", "_", "\\_", "~", "\\~", "[", "\\[", "]", "\\]", "<", "\\<", ">", "\\>", "\n", " ", "\r", " ")
	for i, session := range summary.Community {
		if i == 10 {
			break
		}
		if session.Visibility != collabstore.HostedVisibilityCommunity {
			continue
		}
		title := strings.TrimSpace(session.Title)
		if title == "" {
			title = "Untitled session"
		}
		runes := []rune(title)
		if len(runes) > 64 {
			title = string(runes[:64]) + "…"
		}
		state := "idle"
		if counts[session.SessionID] > 0 {
			state = "active"
		}
		fmt.Fprintf(&text, "• %s — %d participants (%s)\n", escape.Replace(title), counts[session.SessionID], state)
	}
	if summary.CommunityCount > 10 {
		fmt.Fprintf(&text, "…and %d more.\n", summary.CommunityCount-10)
	}
	fmt.Fprintf(&text, "Private sessions: %d (%d active). Private titles and map details are omitted.\nOpen Collaboration → Browse Sessions in AphelionDMM.", summary.PrivateCount, summary.ActivePrivateCount)
	return text.String()
}

type discordSessionReporter struct {
	client                             *http.Client
	baseURL, guildID, channelID, token string
	channelVerified                    bool
	lastContent                        string
}

func (reporter *discordSessionReporter) publish(ctx context.Context, content string) (time.Duration, error) {
	if !reporter.channelVerified {
		body, delay, err := reporter.request(ctx, http.MethodGet, "/channels/"+reporter.channelID, nil)
		if err != nil {
			return delay, err
		}
		var channel struct {
			ID      string `json:"id"`
			GuildID string `json:"guild_id"`
			Type    *int   `json:"type"`
		}
		if json.Unmarshal(body, &channel) != nil || channel.ID != reporter.channelID || channel.GuildID != reporter.guildID || channel.Type == nil || (*channel.Type != 0 && *channel.Type != 5) {
			return 5 * time.Minute, fmt.Errorf("discord notification channel must be a text channel in the configured guild")
		}
		reporter.channelVerified = true
	}
	if content == reporter.lastContent {
		return discordStatusInterval, nil
	}
	body, err := json.Marshal(map[string]any{"content": content, "allowed_mentions": map[string]any{"parse": []string{}}, "flags": 4})
	if err != nil {
		return discordStatusInterval, fmt.Errorf("discord status could not be encoded")
	}
	_, delay, err := reporter.request(ctx, http.MethodPost, "/channels/"+reporter.channelID+"/messages", body)
	if err == nil {
		reporter.lastContent = content
	}
	return delay, err
}

func (reporter *discordSessionReporter) request(ctx context.Context, method, path string, body []byte) ([]byte, time.Duration, error) {
	request, err := http.NewRequestWithContext(ctx, method, reporter.baseURL+path, bytes.NewReader(body))
	if err != nil {
		return nil, discordStatusInterval, fmt.Errorf("discord notification request could not be constructed")
	}
	request.Header.Set("Authorization", "Bot "+reporter.token)
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("User-Agent", "DiscordBot (https://github.com/Aphelion-Moon/AphelionDMM, 1)")
	response, err := reporter.client.Do(request)
	if err != nil {
		return nil, discordStatusInterval, fmt.Errorf("discord notification request failed")
	}
	defer func() { _ = response.Body.Close() }()
	data, err := io.ReadAll(io.LimitReader(response.Body, 64*1024+1))
	if err != nil || len(data) > 64*1024 {
		return nil, discordStatusInterval, fmt.Errorf("discord notification response could not be read within limits")
	}
	if response.StatusCode >= 200 && response.StatusCode < 300 {
		return data, discordStatusInterval, nil
	}
	delay := discordStatusInterval
	if response.StatusCode == http.StatusTooManyRequests {
		var limited struct {
			RetryAfter float64 `json:"retry_after"`
		}
		_ = json.Unmarshal(data, &limited)
		if header, parseErr := strconv.ParseFloat(response.Header.Get("Retry-After"), 64); parseErr == nil && header > limited.RetryAfter {
			limited.RetryAfter = header
		}
		if limited.RetryAfter > 86400 {
			return nil, 24 * time.Hour, fmt.Errorf("discord notifications paused after an excessive rate-limit interval")
		}
		if limited.RetryAfter > delay.Seconds() {
			delay = time.Duration(limited.RetryAfter * float64(time.Second))
		}
	}
	if response.StatusCode == http.StatusUnauthorized || response.StatusCode == http.StatusForbidden {
		delay = 5 * time.Minute
	}
	return nil, delay, fmt.Errorf("discord notifications returned HTTP %d", response.StatusCode)
}
