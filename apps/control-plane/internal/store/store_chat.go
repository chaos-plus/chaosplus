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
	ID            string `bun:"id,pk" json:"id"`
	InstanceID    string `bun:"instance_id,notnull,default:''" json:"instanceId"`
	Name          string `bun:"name,notnull" json:"name"`
	Kind          string `bun:"kind,notnull,default:'digital_human'" json:"kind"`
	Runtime       string `bun:"runtime,notnull,default:'claude'" json:"runtime"`
	Model         string `bun:"model,notnull,default:''" json:"model"`
	Provider      string `bun:"provider,notnull,default:''" json:"provider"`
	SystemPrompt  string `bun:"system_prompt,notnull,default:''" json:"systemPrompt"`
	EntityID      string `bun:"entity_id,notnull,default:''" json:"entityId"`
	OwnerID       string `bun:"owner_id,notnull,default:''" json:"ownerId"`
	Description   string `bun:"description,notnull,default:''" json:"description"`
	// MachineID 是数字人所属的 machine(PRD D.4);空表示未绑定。
	MachineID       string `bun:"machine_id,notnull,default:''" json:"machineId"`
	Status          string `bun:"status,notnull,default:'stopped'" json:"status"` // running | stopped | retired
	DefaultChannels string `bun:"default_channels,notnull,default:''" json:"defaultChannels"`
	HandoverDoc     string `bun:"handover_doc,notnull,default:''" json:"handoverDoc"`
	RetiredAt       int64  `bun:"retired_at,notnull,default:0" json:"retiredAt"`
	CreatedAt       int64  `bun:"created_at,notnull,default:0" json:"createdAt"`
}

func (s *Store) CreateAgent(ctx context.Context, a *AgentSpec) error {
	a.CreatedAt = time.Now().UnixMilli()
	if _, err := s.db.NewInsert().Model(a).Exec(ctx); err != nil {
		return fmt.Errorf("create agent: %w", err)
	}
	return nil
}

func (s *Store) ListAgents(ctx context.Context) ([]AgentSpec, error) {
	out := []AgentSpec{}
	q := s.db.NewSelect().Model(&out)
	if e := EntityOf(ctx); e != "" {
		q = q.Where("entity_id = ?", e)
	}
	// 实体下所有 agent 可见;编辑/管理由 server 层校验 owner。
	if err := q.Order("created_at ASC").Scan(ctx); err != nil {
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
		Set("description = ?", a.Description).Set("machine_id = ?", a.MachineID).
		Set("status = ?", a.Status).Set("default_channels = ?", a.DefaultChannels).Set("owner_id = ?", a.OwnerID).
		Exec(ctx); err != nil {
		return fmt.Errorf("update agent: %w", err)
	}
	return nil
}

// SetAgentStatus 只改生命周期状态(启动/停止),不碰配置字段。
func (s *Store) SetAgentStatus(ctx context.Context, id, status string) error {
	if _, err := s.db.NewUpdate().Model(&AgentSpec{}).Where("id = ?", id).
		Set("status = ?", status).Exec(ctx); err != nil {
		return fmt.Errorf("set agent status: %w", err)
	}
	return nil
}

// RetireAgent 注销数字人:落交接文档 + 置 retired(§6.2.1)。保留记录而不是删除,
// 交接文档必须可查。
func (s *Store) RetireAgent(ctx context.Context, id, handoverDoc string) error {
	if _, err := s.db.NewUpdate().Model(&AgentSpec{}).Where("id = ?", id).
		Set("status = ?", "retired").Set("handover_doc = ?", handoverDoc).
		Set("retired_at = ?", time.Now().UnixMilli()).Exec(ctx); err != nil {
		return fmt.Errorf("retire agent: %w", err)
	}
	return nil
}

// CountAgentsByMachine 统计每台 machine 托管的数字人数(PRD D.3 列表列)。
func (s *Store) CountAgentsByMachine(ctx context.Context) (map[string]int, error) {
	agents, err := s.ListAgents(ctx)
	if err != nil {
		return nil, err
	}
	out := map[string]int{}
	for _, a := range agents {
		if a.MachineID != "" && a.Status != "retired" {
			out[a.MachineID]++
		}
	}
	return out, nil
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
	ID            string `bun:"id,pk" json:"id"`
	InstanceID    string `bun:"instance_id,notnull,default:''" json:"instanceId"`
	Name          string `bun:"name,notnull" json:"name"`
	// OwnerID 是频道创建者;只有 owner 能解散频道。
	OwnerID   string `bun:"owner_id,notnull,default:'human'" json:"ownerId"`
	EntityID  string `bun:"entity_id,notnull,default:''" json:"entityId"`
	CreatedAt int64  `bun:"created_at,notnull,default:0" json:"createdAt"`
}

// DeleteChannel 解散频道:连同成员与消息一起删除,不留孤儿数据。
func (s *Store) DeleteChannel(ctx context.Context, id string) error {
	if _, err := s.db.NewDelete().Model(&ChannelMessage{}).Where("channel_id = ?", id).Exec(ctx); err != nil {
		return fmt.Errorf("delete channel messages: %w", err)
	}
	if _, err := s.db.NewDelete().Model(&ChannelMember{}).Where("channel_id = ?", id).Exec(ctx); err != nil {
		return fmt.Errorf("delete channel members: %w", err)
	}
	if _, err := s.db.NewDelete().Model(&Channel{}).Where("id = ?", id).Exec(ctx); err != nil {
		return fmt.Errorf("delete channel: %w", err)
	}
	return nil
}

func (s *Store) CreateChannel(ctx context.Context, c *Channel) error {
	c.CreatedAt = time.Now().UnixMilli()
	if _, err := s.db.NewInsert().Model(c).Exec(ctx); err != nil {
		return fmt.Errorf("create channel: %w", err)
	}
	return nil
}

func (s *Store) ListChannels(ctx context.Context) ([]Channel, error) {
	out := []Channel{}
	q := s.db.NewSelect().Model(&out)
	if e := EntityOf(ctx); e != "" {
		q = q.Where("entity_id = ?", e)
	}
	// 实体下所有 agent 可见;编辑/管理由 server 层校验 owner。
	if err := q.Order("created_at ASC").Scan(ctx); err != nil {
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

func (s *Store) RemoveChannelMember(ctx context.Context, channelID, memberID, kind string) error {
	if _, err := s.db.NewDelete().Model(&ChannelMember{}).
		Where("channel_id = ? AND member_id = ? AND kind = ?", channelID, memberID, kind).Exec(ctx); err != nil {
		return fmt.Errorf("remove member: %w", err)
	}
	return nil
}

func (s *Store) ListChannelMembers(ctx context.Context, channelID string) ([]ChannelMember, error) {
	out := []ChannelMember{}
	if err := s.db.NewSelect().Model(&out).Where("channel_id = ?", channelID).Scan(ctx); err != nil {
		return nil, fmt.Errorf("list members: %w", err)
	}
	return out, nil
}

// ChannelMember links an agent/human to a channel.
type ChannelMember struct {
	bun.BaseModel `bun:"table:channel_members"`
	ChannelID     string `bun:"channel_id,pk" json:"channelId"`
	MemberID      string `bun:"member_id,pk" json:"memberId"`
	Kind          string `bun:"kind,pk" json:"kind"`
}

// ChannelMessage is one entry in a channel's event-sourced message log.
type ChannelMessage struct {
	bun.BaseModel  `bun:"table:channel_messages"`
	Seq            int64  `bun:"seq,pk,autoincrement" json:"seq"`
	ID             string `bun:"id,notnull,unique" json:"id"`
	ChannelID      string `bun:"channel_id,notnull" json:"channelId"`
	TS             int64  `bun:"ts,notnull" json:"ts"`
	AuthorMemberID string `bun:"author_member_id,notnull,default:''" json:"authorMemberId"`
	AuthorKind     string `bun:"author_kind,notnull,default:'human'" json:"authorKind"`
	IdempotencyKey string `bun:"idempotency_key,notnull,unique" json:"-"`
	PayloadJSON    string `bun:"payload_json,notnull,default:'{}'" json:"payloadJson"`
}

func (s *Store) AppendChannelMessage(ctx context.Context, m *ChannelMessage) error {
	m.TS = time.Now().UnixMilli()
	if _, err := s.db.NewInsert().Model(m).On("CONFLICT (idempotency_key) DO NOTHING").Exec(ctx); err != nil {
		return fmt.Errorf("append message: %w", err)
	}
	return nil
}

func (s *Store) ListChannelMessages(ctx context.Context, channelID string, limit int) ([]ChannelMessage, error) {
	out := []ChannelMessage{}
	q := s.db.NewSelect().Model(&out).Where("channel_id = ?", channelID)
	if limit > 0 {
		q = q.Limit(limit)
	}
	if err := q.Order("seq ASC").Scan(ctx); err != nil {
		return nil, fmt.Errorf("list messages: %w", err)
	}
	return out, nil
}
