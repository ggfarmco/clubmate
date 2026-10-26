package bus

import (
	"regexp"
	"strings"
)

const (
	maxTopicLen = 200
	wildcard    = "*"
	prefixMark  = ".*"
)

var topicRe = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]*(\.[a-z0-9_-]+)*$`)

func ValidTopic(topic string) bool {
	return len(topic) <= maxTopicLen && topicRe.MatchString(topic)
}

func ValidPattern(pattern string) bool {
	if pattern == wildcard {
		return true
	}
	if prefix, ok := strings.CutSuffix(pattern, prefixMark); ok {
		return ValidTopic(prefix)
	}
	return ValidTopic(pattern)
}

func Match(pattern, topic string) bool {
	if pattern == wildcard {
		return true
	}
	if prefix, ok := strings.CutSuffix(pattern, prefixMark); ok {
		return strings.HasPrefix(topic, prefix+".")
	}
	return pattern == topic
}
