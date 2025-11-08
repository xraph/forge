package filters

import (
	"context"
	"regexp"
	"strings"

	streaming "github.com/xraph/forge/extensions/streaming/internal"
)

// ContentFilterConfig configures content filtering.
type ContentFilterConfig struct {
	// Profanity filtering
	EnableProfanity bool
	ProfanityList   []string
	ProfanityAction string // "block", "replace", "flag"
	ReplaceWith     string

	// URL filtering
	EnableURLFilter bool
	AllowedDomains  []string
	BlockedDomains  []string

	// Spam detection
	EnableSpamDetection bool
	MaxURLs             int
	MaxMentions         int
	MaxEmojis           int
	MaxRepeatedChars    int
}

// contentFilter filters message content.
type contentFilter struct {
	config         ContentFilterConfig
	profanityRegex *regexp.Regexp
	urlRegex       *regexp.Regexp
}

// NewContentFilter creates a content filter.
func NewContentFilter(config ContentFilterConfig) MessageFilter {
	cf := &contentFilter{
		config: config,
	}

	// Build profanity regex
	if config.EnableProfanity && len(config.ProfanityList) > 0 {
		pattern := strings.Join(config.ProfanityList, "|")
		cf.profanityRegex = regexp.MustCompile("(?i)\\b(" + pattern + ")\\b")
	}

	// URL regex
	cf.urlRegex = regexp.MustCompile(`https?://[^\s]+`)

	return cf
}

func (cf *contentFilter) Name() string {
	return "content_filter"
}

func (cf *contentFilter) Priority() int {
	return 10 // Early in the chain
}

func (cf *contentFilter) Filter(ctx context.Context, msg *streaming.Message, recipient streaming.EnhancedConnection) (*streaming.Message, error) {
	// Only filter text messages
	text, ok := msg.Data.(string)
	if !ok {
		return msg, nil
	}

	// Profanity filtering
	if cf.config.EnableProfanity && cf.profanityRegex != nil {
		if cf.profanityRegex.MatchString(text) {
			switch cf.config.ProfanityAction {
			case "block":
				return nil, nil // Block message
			case "replace":
				text = cf.profanityRegex.ReplaceAllString(text, cf.config.ReplaceWith)
			case "flag":
				// Add flag to metadata
				if msg.Metadata == nil {
					msg.Metadata = make(map[string]any)
				}

				msg.Metadata["flagged_profanity"] = true
			}
		}
	}

	// URL filtering
	if cf.config.EnableURLFilter {
		urls := cf.urlRegex.FindAllString(text, -1)
		if len(urls) > 0 {
			for _, url := range urls {
				blocked := cf.isURLBlocked(url)
				if blocked {
					return nil, nil // Block message with blocked URL
				}
			}
		}
	}

	// Spam detection
	if cf.config.EnableSpamDetection {
		if cf.isSpam(text) {
			return nil, nil // Block spam
		}
	}

	// Update message with filtered text
	filtered := *msg
	filtered.Data = text

	return &filtered, nil
}

func (cf *contentFilter) isURLBlocked(url string) bool {
	// Check blocked domains
	for _, domain := range cf.config.BlockedDomains {
		if strings.Contains(url, domain) {
			return true
		}
	}

	// If allowed list exists, check it
	if len(cf.config.AllowedDomains) > 0 {
		allowed := false

		for _, domain := range cf.config.AllowedDomains {
			if strings.Contains(url, domain) {
				allowed = true

				break
			}
		}

		return !allowed
	}

	return false
}

func (cf *contentFilter) isSpam(text string) bool {
	// Check URL count
	if cf.config.MaxURLs > 0 {
		urls := cf.urlRegex.FindAllString(text, -1)
		if len(urls) > cf.config.MaxURLs {
			return true
		}
	}

	// Check mentions (@username)
	if cf.config.MaxMentions > 0 {
		mentionRegex := regexp.MustCompile(`@\w+`)

		mentions := mentionRegex.FindAllString(text, -1)
		if len(mentions) > cf.config.MaxMentions {
			return true
		}
	}

	// Check repeated characters
	if cf.config.MaxRepeatedChars > 0 {
		repeatRegex := regexp.MustCompile(`(.)\1{` + string(rune(cf.config.MaxRepeatedChars)) + `,}`)
		if repeatRegex.MatchString(text) {
			return true
		}
	}

	return false
}
