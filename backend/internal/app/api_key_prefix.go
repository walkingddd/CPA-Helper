package app

import (
	"fmt"
	"regexp"
	"strings"
)

// defaultAPIKeyPrefix is used for generated API keys when no prefix is configured. A generated
// key is `<prefix>-<random>`, so the default yields `sk-...` — byte-for-byte the shape produced
// before the prefix became configurable.
const defaultAPIKeyPrefix = "sk"

// maxAPIKeyPrefixLength bounds the configurable prefix so keys stay a sane length.
const maxAPIKeyPrefixLength = 32

// apiKeyPrefixPattern allows letters, digits, `-` and `_`; it must start with a letter or digit
// and must not END with `-` (the generator appends the joining dash itself, so `sk-myteam` is the
// canonical form and `sk-myteam-` would double the dash).
var apiKeyPrefixPattern = regexp.MustCompile(`^[A-Za-z0-9](?:[A-Za-z0-9_-]*[A-Za-z0-9_])?$`)

const (
	apiKeyPrefixTooLongMessage = "api_key_prefix 超出最大长度 32"
	apiKeyPrefixInvalidMessage = "api_key_prefix 只能包含字母、数字、- 和 _，且不能以 - 开头或结尾"
)

// normalizeAPIKeyPrefix trims the configured prefix and substitutes the default for an empty
// value. It does NOT validate — callers that accept user input must call validateAPIKeyPrefix
// first; stored values are already validated.
func normalizeAPIKeyPrefix(value string) string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return defaultAPIKeyPrefix
	}
	return trimmed
}

// validateAPIKeyPrefix rejects a user-supplied prefix that is too long or malformed. An empty
// (or blank) value is accepted and means "use the default".
func validateAPIKeyPrefix(value string) error {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return nil
	}
	if len(trimmed) > maxAPIKeyPrefixLength {
		return validationError(apiKeyPrefixTooLongMessage)
	}
	if !apiKeyPrefixPattern.MatchString(trimmed) {
		return validationError(apiKeyPrefixInvalidMessage)
	}
	return nil
}

// buildAPIKey joins a (normalized) prefix and the random secret with a single dash.
func buildAPIKey(prefix, secret string) string {
	return fmt.Sprintf("%s-%s", prefix, secret)
}
