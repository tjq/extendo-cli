// Package secrets classifies clipboard text that looks like a credential so
// callers can mask it on screen.
//
// It is a port of the macOS app's ContentSensitivity.swift: the patterns, the
// order they are tried in, and the Category raw values are kept identical to
// that file, which stays the source of truth. The heuristics are deliberately
// conservative — they decide how an item is *displayed*, never whether it is
// kept.
package secrets

import (
	"regexp"
	"strings"
	"unicode/utf8"
)

// Category names the kind of secret a text resembles. The values match the raw
// values of Swift's ContentSensitivity.Category. The zero value means "no
// category", which Classify pairs with a false ok.
type Category string

const (
	CategoryCreditCard    Category = "creditCard"
	CategorySSN           Category = "ssn"
	CategoryJWT           Category = "jwt"
	CategoryAWSAccessKey  Category = "awsAccessKey"
	CategoryGitHubToken   Category = "githubToken"
	CategoryPrivateKey    Category = "privateKey"
	CategoryTOTP          Category = "totp"
	CategoryAnthropicKey  Category = "anthropicKey"
	CategoryOpenAIKey     Category = "openaiKey"
	CategoryStripeKey     Category = "stripeKey"
	CategorySlackToken    Category = "slackToken"
	CategoryGenericAPIKey Category = "genericApiKey"
)

// Label returns the human-readable name shown next to a masked item.
func (c Category) Label() string {
	switch c {
	case CategoryCreditCard:
		return "Credit card"
	case CategorySSN:
		return "SSN"
	case CategoryJWT:
		return "JWT"
	case CategoryAWSAccessKey:
		return "AWS key"
	case CategoryGitHubToken:
		return "GitHub token"
	case CategoryPrivateKey:
		return "Private key"
	case CategoryTOTP:
		return "TOTP code"
	case CategoryAnthropicKey:
		return "Anthropic key"
	case CategoryOpenAIKey:
		return "OpenAI key"
	case CategoryStripeKey:
		return "Stripe key"
	case CategorySlackToken:
		return "Slack token"
	case CategoryGenericAPIKey:
		return "API key"
	default:
		return string(c)
	}
}

// pattern is one detection rule.
type pattern struct {
	category Category
	re       *regexp.Regexp
	// isWholeOnly requires the match to consume the entire trimmed text, for
	// short standalone codes like a TOTP where the whole clipboard is the
	// secret.
	isWholeOnly bool
}

// maxScanRunes mirrors the app's guard: absurdly large inputs are skipped
// rather than fed to the regex engine at render time.
const maxScanRunes = 64 * 1024

// patterns is tried in order, so the specific rules label an item before the
// generic "looks like an API key" rule can. The order is the declaration order
// in ContentSensitivity.swift and must not be rearranged.
var patterns = []pattern{
	// Credit cards: 13-19 digits, optionally grouped by spaces/hyphens. No
	// Luhn check — this matches what people actually copy.
	{category: CategoryCreditCard, re: regexp.MustCompile(`\b(?:\d[ -]?){12,18}\d\b`)},
	// US SSN.
	{category: CategorySSN, re: regexp.MustCompile(`\b\d{3}-\d{2}-\d{4}\b`)},
	// JWTs (three base64url segments).
	{
		category: CategoryJWT,
		re:       regexp.MustCompile(`\beyJ[A-Za-z0-9_-]{8,}\.[A-Za-z0-9_-]{8,}\.[A-Za-z0-9_-]{8,}\b`),
	},
	// AWS access key IDs.
	{category: CategoryAWSAccessKey, re: regexp.MustCompile(`\b(?:AKIA|ASIA)[0-9A-Z]{16}\b`)},
	// GitHub personal access tokens / fine-grained tokens.
	{
		category: CategoryGitHubToken,
		re: regexp.MustCompile(`\b(?:ghp_[A-Za-z0-9]{36}|github_pat_[A-Za-z0-9_]{82}|` +
			`gho_[A-Za-z0-9]{36}|ghu_[A-Za-z0-9]{36}|ghs_[A-Za-z0-9]{36}|ghr_[A-Za-z0-9]{36})\b`),
	},
	// Anthropic API keys.
	{category: CategoryAnthropicKey, re: regexp.MustCompile(`\bsk-ant-[A-Za-z0-9_-]{20,}\b`)},
	// OpenAI API keys (legacy + project-scoped).
	{category: CategoryOpenAIKey, re: regexp.MustCompile(`\bsk-(?:proj-)?[A-Za-z0-9_-]{20,}\b`)},
	// Stripe live/test keys.
	{category: CategoryStripeKey, re: regexp.MustCompile(`\b(?:sk|pk|rk)_(?:live|test)_[A-Za-z0-9]{24,}\b`)},
	// Slack tokens.
	{category: CategorySlackToken, re: regexp.MustCompile(`\bxox[abprs]-[A-Za-z0-9-]{10,}\b`)},
	// PEM-style private key blocks.
	{
		category: CategoryPrivateKey,
		re:       regexp.MustCompile(`-----BEGIN (?:RSA |EC |DSA |OPENSSH |PGP |ENCRYPTED )?PRIVATE KEY(?: BLOCK)?-----`),
	},
	// 6-8 digit TOTP shown on its own. (?m) mirrors the Swift regex's
	// .anchorsMatchLines option; isWholeOnly then rejects a match that does
	// not cover the whole clipboard.
	{category: CategoryTOTP, re: regexp.MustCompile(`(?m)^\d{6,8}$`), isWholeOnly: true},
	// Generic "looks like an API key" — a key-ish label plus a long
	// entropy-looking tail. Deliberately last so the patterns above win.
	{
		category: CategoryGenericAPIKey,
		re:       regexp.MustCompile(`(?i)\b(?:api[_-]?key|secret|token|password)\s*[:=]\s*['"]?[A-Za-z0-9_\-]{20,}['"]?`),
	},
}

// Classify reports the first category matching text, in the pattern order
// above. It returns false when nothing matches, when text is blank, or when
// text is too large to scan.
func Classify(text string) (Category, bool) {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" || utf8.RuneCountInString(trimmed) > maxScanRunes {
		return "", false
	}

	for _, p := range patterns {
		match := p.re.FindStringIndex(trimmed)
		if match == nil {
			continue
		}

		// Like the Swift version, only the leftmost match is considered: a
		// whole-only pattern whose first match is a substring does not hit.
		if p.isWholeOnly && (match[0] != 0 || match[1] != len(trimmed)) {
			continue
		}

		return p.category, true
	}

	return "", false
}

const (
	maskShort       = "••••••••"   // stands in for the whole value
	maskTail        = "••••••••••" // follows a revealed prefix
	maskPrefixRunes = 7
	maskMinRunes    = 8
)

// Mask renders text as a preview safe to show on screen. Values of 8 characters
// or fewer are hidden entirely, since a prefix of a short secret gives too much
// away; longer ones keep the first few runes of their first line for
// recognition.
func Mask(text string) string {
	if utf8.RuneCountInString(text) <= maskMinRunes {
		return maskShort
	}

	firstLine := text
	if end := strings.IndexAny(text, "\r\n"); end >= 0 {
		firstLine = text[:end]
	}

	prefix := []rune(firstLine)
	if len(prefix) > maskPrefixRunes {
		prefix = prefix[:maskPrefixRunes]
	}

	return string(prefix) + maskTail
}
