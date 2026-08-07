package utils

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestExecutableName(t *testing.T) {
	assert.NotEmpty(t, GetExecutableName())
}
