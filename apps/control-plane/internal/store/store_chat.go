package store

import (
	"context"
	"fmt"
	"time"

	"github.com/uptrace/bun"
)

// AgentSpec is a team agent (digital_human or executor) that can join channels
// and be invoked by chat messages.
type AgentSpec struct {
	bun.BaseModel `bun:"table:agent_specs"`
	ID            string `bun:"id,pk"`
	InstanceID    string `bun:"instance_id,notnull,default:''"`
	Name          string `bun:"name,notnull"`
	Kind          string `bun:"kind,notnull,default:'digital_human'"`
	Runtime       string `bun:"runtime,notnull,default:'claude'"`
	Model         string `bun:"model,notnull,default:''"`
	Provider      string `bun:"provider,notnull,default:''"`
	SystemPrompt  string `bun:"system_prompt,notnull,default:''"`
	CreatedAt     int64  `bun:"created_at,notnull,default:0"`
}

func (s *Store) CreateAgent(ctx context.Context, a *AgentSpec) error {
	a.CreatedAt = time.Now().UnixMilli()
	if _, err := s.db.NewInsert().Model(a).Exec(ctx); err != nil {
		return fmt.Errorf("create agent: %w", err)
	}
	return nil
}

func (s *Store) ListAgents(ctx context.Context) ([]AgentSpec, error) {
	var out []AgentSpec
	if err := s.db.NewSelect().Model(&out).Order("created_at ASC").Scan(ctx); err != nil {
		return nil, fmt.Errorf("list agents: %w", err)
	}
	return out, nil
}

func (s *Store) GetAgent(ctx context.Context, id string) (*AgentSpec, error) {
	var a AgentSpec
	if err := s.db.NewSelect().Model(&a).Where("id = ?", id).Scan(ctx); err != nil {
		return nil, fmt.Errorf("get agent: %w", err)
	}
	return &a, nil
}

func (s *Store) UpdateAgent(ctx context.Context, a *AgentSpec) error {
	if _, err := s.db.NewUpdate().Model(a).Where("id = ?", a.ID).
		Set("name = ?", a.Name).Set("kind = ?", a.Kind).
		Set("runtime = ?", a.Runtime).Set("model = ?", a.Model).
		Set("provider = ?", a.Provider).Set("system_prompt = ?", a.SystemPrompt).
		Exec(ctx); err != nil {
		return fmt.Errorf("update agent: %w", err)
	}
	return nil
}

func (s *Store) DeleteAgent(ctx context.Context, id string) error {
	if _, err := s.db.NewDelete().Model(&AgentSpec{}).Where("id = ?", id).Exec(ctx); err != nil {
		return fmt.Errorf("delete agent: %w", err)
	}
	return nil
}

// Channel is a conversation room (§9 ConversationHub). Channels hold members
// (agents/humans) and an event-sourced message log.
type Channel struct {
	bun.BaseModel `bun:"table:channels"`
	ID           string `bun:"id,pk"`
	InstanceID   string `bun:"instance_id,notnull,default:''"`
	Name         string `bun:"name,notnull"`
	CreatedAt    int64  `bun:"created_at,notnull,default:0"`
}

func (s *Store) CreateChannel(ctx context.Context, c *Channel) error {
	c.CreatedAt = time.Now().UnixMilli()
	if _, err := s.db.NewInsert().Model(c).Exec(ctx); err != nil {
		return fmt.Errorf("create channel: %w", err)
	}
	return nil
}

func (s *Store) ListChannels(ctx context.Context) ([]Channel, error) {
	var out []Channel
	if err := s.db.NewSelect().Model(&out).Order("created_at ASC").Scan(ctx); err != nil {
		return nil, fmt.Errorf("list channels: %w", err)
	}
	return out, nil
}

func (s *Store) GetChannel(ctx context.Context, id string) (*Channel, error) {
	var c Channel
	if err := s.db.NewSelect().Model(&c).Where("id = ?", id).Scan(ctx); err != nil {
		return nil, fmt.Errorf("get channel: %w", err)
	}
	return &c, nil
}

func (s *Store) AddChannelMember(ctx context.Context, channelID, memberID, kind string) error {
	if _, err := s.db.NewInsert().Model(&ChannelMember{ChannelID: channelID, MemberID: memberID, Kind: kind}).
		On("CONFLICT DO NOTHING").Exec(ctx); err != nil {
		return fmt.Errorf("add member: %w", err)
	}
	return nil
}

func (s *Store) ListChannelMembers(ctx context.Context, channelID string) ([]ChannelMember, error) {
	var out []ChannelMember
	if err := s.db.NewSelect().Model(&out).Where("channel_id = ?", channelID).Scan(ctx); err != nil {
		return nil, fmt.Errorf("list members: %w", err)
	}
	return out, nil
}

// ChannelMember links an agent/human to a channel.
type ChannelMember struct {
	bun.BaseModel `bun:"table:channel_members"`
	ChannelID     string `bun:"channel_id,pk"`
	MemberID      string `bun:"member_id,pk"`
	Kind          string `bun:"kind,pk"`
}

// ChannelMessage is one entry in a channel's event-sourced message log.
type ChannelMessage struct {
	bun.BaseModel  `bun:"table:channel_messages"`
	Seq            int64  `bun:"seq,pk,autoincrement"`
	ID             string `bun:"id,notnull,unique"`
	ChannelID      string `bun:"channel_id,notnull"`
	TS             int64  `bun:"ts,notnull"`
	AuthorMemberID string `bun:"author_member_id,notnull,default:''"`
	AuthorKind     string `bun:"author_kind,notnull,default:'human'"`
	IdempotencyKey string `bun:"idempotency_key,notnull,unique"`
	PayloadJSON    string `bun:"payload_json,notnull,default:'{}'"`
}

func (s *Store) AppendChannelMessage(ctx context.Context, m *ChannelMessage) error {
	m.TS = time.Now().UnixMilli()
	if _, err := s.db.NewInsert().Model(m).On("CONFLICT (idempotency_key) DO NOTHING").Exec(ctx); err != nil {
		return fmt.Errorf("append message: %w", err)
	}
	return nil
}

func (s *Store) ListChannelMessages(ctx context.Context, channelID string, limit int) ([]ChannelMessage, error) {
	var out []ChannelMessage
	q := s.db.NewSelect().Model(&out).Where("channel_id = ?", channelID)
	if limit > 0 {
		q = q.Limit(limit)
	}
	if err := q.Order("seq ASC").Scan(ctx); err != nil {
		return nil, fmt.Errorf("list messages: %w", err)
	}
	return out, nil
}
