package core

import (
	"errors"
	"strings"
)

// Topics are "<object_type>/<id_segment>[/<id_segment>...]" with each segment
// non-empty and made of URL-safe characters. The first segment picks the
// reducer.
var ErrInvalidTopic = errors.New("invalid topic")

func ParseObjectType(topic string) (string, error) {
	if topic == "" {
		return "", ErrInvalidTopic
	}
	i := strings.IndexByte(topic, '/')
	if i <= 0 || i == len(topic)-1 {
		return "", ErrInvalidTopic
	}
	return topic[:i], nil
}

func ValidateTopic(topic string) error {
	if topic == "" || len(topic) > 512 {
		return ErrInvalidTopic
	}
	parts := strings.Split(topic, "/")
	if len(parts) < 2 {
		return ErrInvalidTopic
	}
	for _, p := range parts {
		if p == "" {
			return ErrInvalidTopic
		}
		for _, r := range p {
			switch {
			case r >= 'a' && r <= 'z',
				r >= 'A' && r <= 'Z',
				r >= '0' && r <= '9',
				r == '-', r == '_', r == '.':
			default:
				return ErrInvalidTopic
			}
		}
	}
	return nil
}
