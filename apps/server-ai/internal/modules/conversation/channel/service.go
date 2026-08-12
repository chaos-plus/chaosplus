package channel

import (
	"context"

	"github.com/chaos-plus/chaosplus/internal/core/extension/authn"
	"github.com/chaos-plus/chaosplus/internal/infra/guid"
)

type Service struct {
	repository Repository
	agents     AgentDirectory
}

func NewService(repository Repository, agents AgentDirectory) *Service {
	if repository == nil || agents == nil {
		panic("channel service requires repository and agent directory")
	}
	return &Service{repository: repository, agents: agents}
}

func (s *Service) Create(ctx context.Context, input ChannelCreateInput) (*Channel, error) {
	value := &Channel{ProjectID: input.ProjectID, Name: input.Name, Topic: input.Topic, Status: StatusActive}
	if err := validateChannel(value); err != nil {
		return nil, err
	}
	owner := new(Member)
	if err := s.repository.Create(ctx, value, owner); err != nil {
		return nil, err
	}
	return value, nil
}

func (s *Service) List(ctx context.Context, projectID guid.ID) ([]Channel, error) {
	return s.repository.List(ctx, projectID)
}

func (s *Service) Get(ctx context.Context, id guid.ID) (*Channel, error) {
	if id.Zero() {
		return nil, ErrInvalid
	}
	return s.repository.Get(ctx, id)
}

func (s *Service) Update(ctx context.Context, id guid.ID, input ChannelUpdateInput) (*Channel, error) {
	if id.Zero() || input.Version < 1 {
		return nil, ErrInvalid
	}
	value, err := s.requireOwner(ctx, id)
	if err != nil {
		return nil, err
	}
	if value.Version != input.Version {
		return nil, ErrVersionConflict
	}
	if input.Name != nil {
		value.Name = *input.Name
	}
	if input.Topic != nil {
		value.Topic = *input.Topic
	}
	if input.Status != nil {
		value.Status = *input.Status
	}
	if err := validateChannel(value); err != nil {
		return nil, err
	}
	if err := s.repository.Update(ctx, value, input.Version); err != nil {
		return nil, err
	}
	return value, nil
}

func (s *Service) Delete(ctx context.Context, id guid.ID, version int64) error {
	if id.Zero() || version < 1 {
		return ErrInvalid
	}
	value, err := s.requireOwner(ctx, id)
	if err != nil {
		return err
	}
	if value.Version != version {
		return ErrVersionConflict
	}
	return s.repository.Delete(ctx, id, version)
}

func (s *Service) AddMember(ctx context.Context, channelID guid.ID, input AddMemberInput) (*Member, error) {
	if channelID.Zero() || input.MemberID.Zero() || (input.Kind != MemberHuman && input.Kind != MemberAgent) {
		return nil, ErrInvalid
	}
	if _, err := s.requireOwner(ctx, channelID); err != nil {
		return nil, err
	}
	principalID := authn.PrincipalIDFromContext(ctx)
	switch input.Kind {
	case MemberHuman:
		if input.MemberID != principalID {
			return nil, ErrForbidden
		}
	case MemberAgent:
		available, err := s.agents.IsAvailable(ctx, input.MemberID)
		if err != nil {
			return nil, err
		}
		if !available {
			return nil, ErrInvalid
		}
	}
	value := &Member{ChannelID: channelID, MemberID: input.MemberID, Kind: input.Kind}
	if err := s.repository.AddMember(ctx, value); err != nil {
		return nil, err
	}
	return value, nil
}

func (s *Service) RemoveMember(ctx context.Context, channelID, memberID guid.ID, kind MemberKind, version int64) error {
	if channelID.Zero() || memberID.Zero() || version < 1 || (kind != MemberHuman && kind != MemberAgent) {
		return ErrInvalid
	}
	if _, err := s.requireOwner(ctx, channelID); err != nil {
		return err
	}
	return s.repository.RemoveMember(ctx, channelID, memberID, kind, version)
}

func (s *Service) ListMembers(ctx context.Context, channelID guid.ID) ([]Member, error) {
	if _, err := s.Get(ctx, channelID); err != nil {
		return nil, err
	}
	return s.repository.ListMembers(ctx, channelID)
}

func (s *Service) IsMember(ctx context.Context, channelID, memberID guid.ID, kind MemberKind) (bool, error) {
	if _, err := s.Get(ctx, channelID); err != nil {
		return false, err
	}
	return s.repository.IsMember(ctx, channelID, memberID, kind)
}

func (s *Service) requireOwner(ctx context.Context, id guid.ID) (*Channel, error) {
	value, err := s.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	if value.OwnerID != authn.PrincipalIDFromContext(ctx) {
		return nil, ErrForbidden
	}
	return value, nil
}
