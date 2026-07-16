package authn

import (
	"errors"
	"strings"
)

var (
	ErrMissingBearer = errors.New("missing bearer token")
	ErrInvalidBearer = errors.New("invalid bearer token")
)

func bearerToken(header string) (string, error) {
	if strings.TrimSpace(header) == "" {
		return "", ErrMissingBearer
	}
	fields := strings.Fields(header)
	if len(fields) != 2 || !strings.EqualFold(fields[0], "Bearer") {
		return "", ErrInvalidBearer
	}
	return fields[1], nil
}
