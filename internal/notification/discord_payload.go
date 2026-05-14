package notification

import (
	"sort"

	"github.com/RedBoardDev/gh-runners-tool/v2/internal/model"
)

type discordPayload struct {
	Username  string         `json:"username,omitempty"`
	AvatarURL string         `json:"avatar_url,omitempty"`
	Content   string         `json:"content,omitempty"`
	Embeds    []discordEmbed `json:"embeds"`
}

type discordEmbed struct {
	Title       string         `json:"title"`
	Description string         `json:"description"`
	Color       int            `json:"color"`
	Fields      []discordField `json:"fields,omitempty"`
	Footer      *discordFooter `json:"footer,omitempty"`
	Timestamp   string         `json:"timestamp,omitempty"`
}

type discordField struct {
	Name   string `json:"name"`
	Value  string `json:"value"`
	Inline bool   `json:"inline"`
}

type discordFooter struct {
	Text string `json:"text"`
}

func (d *DiscordProvider) buildPayload(event *model.Event) discordPayload {
	fields := d.buildFields(event)
	embed := discordEmbed{
		Title:       event.Type,
		Description: event.Message,
		Color:       colorForLevel(event.Level),
		Fields:      fields,
		Footer:      &discordFooter{Text: "ghr"},
		Timestamp:   event.Timestamp.UTC().Format("2006-01-02T15:04:05Z"),
	}

	payload := discordPayload{
		Username:  d.cfg.Username,
		AvatarURL: d.cfg.AvatarURL,
		Embeds:    []discordEmbed{embed},
	}

	mention := d.mentionForLevel(event.Level)
	if mention != "" {
		payload.Content = mention
	}

	return payload
}

func (d *DiscordProvider) buildFields(event *model.Event) []discordField {
	var fields []discordField

	if event.Group != "" {
		fields = append(fields, discordField{Name: "Group", Value: event.Group, Inline: true})
	}
	if event.Runner != "" {
		fields = append(fields, discordField{Name: "Runner", Value: event.Runner, Inline: true})
	}

	keys := make([]string, 0, len(event.Details))
	for k := range event.Details {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	for _, k := range keys {
		fields = append(fields, discordField{Name: k, Value: event.Details[k], Inline: false})
	}

	return fields
}

func (d *DiscordProvider) mentionForLevel(level model.EventLevel) string {
	switch level {
	case model.LevelError:
		return d.cfg.Mentions.Error
	case model.LevelCritical:
		return d.cfg.Mentions.Critical
	default:
		return ""
	}
}

func colorForLevel(level model.EventLevel) int {
	switch level {
	case model.LevelInfo:
		return 0x3498DB
	case model.LevelWarning:
		return 0xF39C12
	case model.LevelError:
		return 0xE74C3C
	case model.LevelCritical:
		return 0x992D22
	default:
		return 0x3498DB
	}
}
