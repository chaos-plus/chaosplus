// Package conversation composes independently owned conversation leaves.
package conversation

import (
	"errors"

	"github.com/chaos-plus/chaosplus/apps/server-ai/internal/modules/conversation/agent"
	"github.com/chaos-plus/chaosplus/apps/server-ai/internal/modules/conversation/channel"
	"github.com/chaos-plus/chaosplus/apps/server-ai/internal/modules/conversation/message"
	"github.com/chaos-plus/chaosplus/internal/core/extension/authz"
)

func Actions() []authz.Action {
	result := make([]authz.Action, 0, len(agent.Actions)+len(channel.Actions)+len(message.Actions))
	result = append(result, agent.Actions...)
	result = append(result, channel.Actions...)
	result = append(result, message.Actions...)
	return result
}

func RegisterI18n() error {
	return errors.Join(agent.RegisterI18n(), channel.RegisterI18n(), message.RegisterI18n())
}
