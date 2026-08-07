package provisioning

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/chaos-plus/chaosplus/internal/core/extension/auditx"
	"github.com/chaos-plus/chaosplus/internal/core/extension/passwordx"
	"github.com/chaos-plus/chaosplus/internal/modules/identity"
	"github.com/chaos-plus/chaosplus/internal/modules/organization"
	"github.com/uptrace/bun"
)

func (s *Service) CreateUser(ctx context.Context, auth AuthContext, input UserInput) (UserResource, error) {
	input, active, err := normalizeUserInput(input)
	if err != nil {
		return UserResource{}, err
	}
	var resourceID string
	err = s.db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		repo := s.repo.withExecutor(tx)
		if input.ExternalID != "" {
			mapping, err := repo.getResourceByExternalKey(ctx, auth.DirectoryID, ResourceUser, externalKey(input.ExternalID, ""))
			switch {
			case err == nil && mapping.DeletedAt == 0:
				return ErrResourceConflict
			case err == nil:
				resourceID = mapping.ResourceID
				if _, err := s.identity.ReplaceProvisionedTo(ctx, tx, auth.TenantID, resourceID, provisionedUser(input, active, "")); err != nil {
					return err
				}
				now := s.now().UTC().UnixMilli()
				mapping.ExternalID, mapping.ExternalKey, mapping.Version = input.ExternalID, externalKey(input.ExternalID, resourceID), mapping.Version+1
				mapping.UpdatedAt, mapping.DeletedAt = now, 0
				if err := repo.updateResource(ctx, &mapping, mapping.Version-1); err != nil {
					return err
				}
				return s.appendResourceAudit(ctx, tx, auth, "scim_user_restored", ResourceUser, resourceID)
			case !errors.Is(err, ErrResourceMissing):
				return err
			}
		}
		secret, err := randomTokenSecret()
		if err != nil {
			return err
		}
		hash, err := passwordx.Hash(secret)
		if err != nil {
			return err
		}
		principal, err := s.identity.CreateProvisionedTo(ctx, tx, auth.TenantID, provisionedUser(input, active, hash))
		if err != nil {
			return err
		}
		resourceID = principal.ID
		now := s.now().UTC().UnixMilli()
		mapping := resourceRow{DirectoryID: auth.DirectoryID, ResourceType: ResourceUser, ResourceID: resourceID, ExternalID: input.ExternalID, ExternalKey: externalKey(input.ExternalID, resourceID), Version: 1, CreatedAt: now, UpdatedAt: now}
		if err := repo.insertResource(ctx, &mapping); err != nil {
			return err
		}
		return s.appendResourceAudit(ctx, tx, auth, "scim_user_created", ResourceUser, resourceID)
	})
	if err != nil {
		return UserResource{}, fmt.Errorf("create SCIM user: %w", err)
	}
	return s.GetUser(ctx, auth, resourceID)
}

func (s *Service) GetUser(ctx context.Context, auth AuthContext, id string) (UserResource, error) {
	record, err := s.repo.user(ctx, auth.DirectoryID, strings.TrimSpace(id))
	if err != nil {
		return UserResource{}, err
	}
	return userResource(record), nil
}

func (s *Service) ListUsers(ctx context.Context, auth AuthContext, request ListRequest) (ListResponse[UserResource], error) {
	rows, total, err := s.repo.userPage(ctx, auth.DirectoryID, request, normalizeDialect(s.db))
	if err != nil {
		return ListResponse[UserResource]{}, err
	}
	resources := make([]UserResource, 0, len(rows))
	for _, row := range rows {
		resources = append(resources, userResource(row))
	}
	return ListResponse[UserResource]{Schemas: []string{ListSchema}, TotalResults: total, StartIndex: request.StartIndex, ItemsPerPage: len(resources), Resources: resources}, nil
}

func (s *Service) ReplaceUser(ctx context.Context, auth AuthContext, id string, input UserInput, expectedVersion int64) (UserResource, error) {
	input, active, err := normalizeUserInput(input)
	id = strings.TrimSpace(id)
	if err != nil || id == "" {
		return UserResource{}, ErrInvalidSCIM
	}
	err = s.db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		repo := s.repo.withExecutor(tx)
		mapping, err := repo.getResource(ctx, auth.DirectoryID, ResourceUser, id, false)
		if err != nil {
			return err
		}
		if expectedVersion > 0 && mapping.Version != expectedVersion {
			return ErrResourceVersion
		}
		if _, err := s.identity.ReplaceProvisionedTo(ctx, tx, auth.TenantID, id, provisionedUser(input, active, "")); err != nil {
			return err
		}
		now := s.now().UTC().UnixMilli()
		previous := mapping.Version
		mapping.ExternalID, mapping.ExternalKey, mapping.Version = input.ExternalID, externalKey(input.ExternalID, id), previous+1
		mapping.UpdatedAt = now
		if err := repo.updateResource(ctx, &mapping, previous); err != nil {
			return err
		}
		return s.appendResourceAudit(ctx, tx, auth, "scim_user_replaced", ResourceUser, id)
	})
	if err != nil {
		return UserResource{}, fmt.Errorf("replace SCIM user: %w", err)
	}
	return s.GetUser(ctx, auth, id)
}

func (s *Service) DeleteUser(ctx context.Context, auth AuthContext, id string, expectedVersion int64) error {
	id = strings.TrimSpace(id)
	if id == "" {
		return ErrInvalidSCIM
	}
	err := s.db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		repo := s.repo.withExecutor(tx)
		record, err := repo.user(ctx, auth.DirectoryID, id)
		if err != nil {
			return err
		}
		if expectedVersion > 0 && record.Version != expectedVersion {
			return ErrResourceVersion
		}
		input := identity.ProvisionedPrincipalInput{LoginName: record.LoginName, DisplayName: record.DisplayName, Email: record.Email, Active: false}
		if _, err := s.identity.ReplaceProvisionedTo(ctx, tx, auth.TenantID, id, input); err != nil {
			return err
		}
		now, previous := s.now().UTC().UnixMilli(), record.Version
		record.Version, record.UpdatedAt, record.DeletedAt = previous+1, now, now
		if err := repo.updateResource(ctx, &record.resourceRow, previous); err != nil {
			return err
		}
		return s.appendResourceAudit(ctx, tx, auth, "scim_user_deleted", ResourceUser, id)
	})
	if err != nil {
		return fmt.Errorf("delete SCIM user: %w", err)
	}
	return nil
}

func (s *Service) CreateGroup(ctx context.Context, auth AuthContext, input GroupInput) (GroupResource, error) {
	input, members, err := normalizeGroupInput(input)
	if err != nil {
		return GroupResource{}, err
	}
	var resourceID string
	err = s.db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		repo := s.repo.withExecutor(tx)
		if err := requireSCIMUsers(ctx, repo, auth.DirectoryID, members); err != nil {
			return err
		}
		if input.ExternalID != "" {
			mapping, err := repo.getResourceByExternalKey(ctx, auth.DirectoryID, ResourceGroup, externalKey(input.ExternalID, ""))
			switch {
			case err == nil && mapping.DeletedAt == 0:
				return ErrResourceConflict
			case err == nil:
				resourceID = mapping.ResourceID
				if _, err := s.groups.ReplaceProvisionedTo(ctx, tx, auth.TenantID, resourceID, provisionedGroup(input, members, true)); err != nil {
					return err
				}
				now, previous := s.now().UTC().UnixMilli(), mapping.Version
				mapping.ExternalID, mapping.ExternalKey, mapping.Version = input.ExternalID, externalKey(input.ExternalID, resourceID), previous+1
				mapping.UpdatedAt, mapping.DeletedAt = now, 0
				if err := repo.updateResource(ctx, &mapping, previous); err != nil {
					return err
				}
				return s.appendResourceAudit(ctx, tx, auth, "scim_group_restored", ResourceGroup, resourceID)
			case !errors.Is(err, ErrResourceMissing):
				return err
			}
		}
		group, err := s.groups.CreateProvisionedTo(ctx, tx, auth.TenantID, provisionedGroup(input, members, true))
		if err != nil {
			return err
		}
		resourceID = group.ID
		now := s.now().UTC().UnixMilli()
		mapping := resourceRow{DirectoryID: auth.DirectoryID, ResourceType: ResourceGroup, ResourceID: resourceID, ExternalID: input.ExternalID, ExternalKey: externalKey(input.ExternalID, resourceID), Version: 1, CreatedAt: now, UpdatedAt: now}
		if err := repo.insertResource(ctx, &mapping); err != nil {
			return err
		}
		return s.appendResourceAudit(ctx, tx, auth, "scim_group_created", ResourceGroup, resourceID)
	})
	if err != nil {
		return GroupResource{}, fmt.Errorf("create SCIM group: %w", err)
	}
	return s.GetGroup(ctx, auth, resourceID)
}

func (s *Service) GetGroup(ctx context.Context, auth AuthContext, id string) (GroupResource, error) {
	record, err := s.repo.group(ctx, auth.DirectoryID, strings.TrimSpace(id))
	if err != nil {
		return GroupResource{}, err
	}
	members, err := s.repo.groupMembers(ctx, auth.DirectoryID, record.ResourceID)
	if err != nil {
		return GroupResource{}, err
	}
	return groupResource(record, members), nil
}

func (s *Service) ListGroups(ctx context.Context, auth AuthContext, request ListRequest) (ListResponse[GroupResource], error) {
	rows, total, err := s.repo.groupPage(ctx, auth.DirectoryID, request, normalizeDialect(s.db))
	if err != nil {
		return ListResponse[GroupResource]{}, err
	}
	resources := make([]GroupResource, 0, len(rows))
	for _, row := range rows {
		members, err := s.repo.groupMembers(ctx, auth.DirectoryID, row.ResourceID)
		if err != nil {
			return ListResponse[GroupResource]{}, err
		}
		resources = append(resources, groupResource(row, members))
	}
	return ListResponse[GroupResource]{Schemas: []string{ListSchema}, TotalResults: total, StartIndex: request.StartIndex, ItemsPerPage: len(resources), Resources: resources}, nil
}

func (s *Service) ReplaceGroup(ctx context.Context, auth AuthContext, id string, input GroupInput, expectedVersion int64) (GroupResource, error) {
	input, members, err := normalizeGroupInput(input)
	id = strings.TrimSpace(id)
	if err != nil || id == "" {
		return GroupResource{}, ErrInvalidSCIM
	}
	err = s.db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		repo := s.repo.withExecutor(tx)
		mapping, err := repo.getResource(ctx, auth.DirectoryID, ResourceGroup, id, false)
		if err != nil {
			return err
		}
		if expectedVersion > 0 && mapping.Version != expectedVersion {
			return ErrResourceVersion
		}
		if err := requireSCIMUsers(ctx, repo, auth.DirectoryID, members); err != nil {
			return err
		}
		if _, err := s.groups.ReplaceProvisionedTo(ctx, tx, auth.TenantID, id, provisionedGroup(input, members, true)); err != nil {
			return err
		}
		now, previous := s.now().UTC().UnixMilli(), mapping.Version
		mapping.ExternalID, mapping.ExternalKey, mapping.Version = input.ExternalID, externalKey(input.ExternalID, id), previous+1
		mapping.UpdatedAt = now
		if err := repo.updateResource(ctx, &mapping, previous); err != nil {
			return err
		}
		return s.appendResourceAudit(ctx, tx, auth, "scim_group_replaced", ResourceGroup, id)
	})
	if err != nil {
		return GroupResource{}, fmt.Errorf("replace SCIM group: %w", err)
	}
	return s.GetGroup(ctx, auth, id)
}

func (s *Service) DeleteGroup(ctx context.Context, auth AuthContext, id string, expectedVersion int64) error {
	id = strings.TrimSpace(id)
	if id == "" {
		return ErrInvalidSCIM
	}
	err := s.db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		repo := s.repo.withExecutor(tx)
		mapping, err := repo.getResource(ctx, auth.DirectoryID, ResourceGroup, id, false)
		if err != nil {
			return err
		}
		if expectedVersion > 0 && mapping.Version != expectedVersion {
			return ErrResourceVersion
		}
		if _, err := s.groups.DisableProvisionedTo(ctx, tx, auth.TenantID, id); err != nil {
			return err
		}
		now, previous := s.now().UTC().UnixMilli(), mapping.Version
		mapping.Version, mapping.UpdatedAt, mapping.DeletedAt = previous+1, now, now
		if err := repo.updateResource(ctx, &mapping, previous); err != nil {
			return err
		}
		return s.appendResourceAudit(ctx, tx, auth, "scim_group_deleted", ResourceGroup, id)
	})
	if err != nil {
		return fmt.Errorf("delete SCIM group: %w", err)
	}
	return nil
}

func (s *Service) appendResourceAudit(ctx context.Context, db bun.IDB, auth AuthContext, eventType, resourceType, resourceID string) error {
	event := auditx.NewEvent(ctx, auth.TenantID, eventType, "scim_"+strings.ToLower(resourceType), resourceID)
	event.Detail["directory_id"] = auth.DirectoryID
	event.Detail["credential_id"] = auth.CredentialID
	return s.audit(ctx, db, event)
}

func provisionedUser(input UserInput, active bool, passwordHash string) identity.ProvisionedPrincipalInput {
	email := ""
	if len(input.Emails) > 0 {
		email = input.Emails[0].Value
	}
	return identity.ProvisionedPrincipalInput{LoginName: input.UserName, DisplayName: input.DisplayName, Email: email, PasswordHash: passwordHash, Active: active}
}

func provisionedGroup(input GroupInput, members []string, active bool) organization.ProvisionedGroupInput {
	return organization.ProvisionedGroupInput{DisplayName: input.DisplayName, Active: active, MemberIDs: members}
}

func requireSCIMUsers(ctx context.Context, repo *Repository, directoryID string, ids []string) error {
	for _, id := range ids {
		row, err := repo.user(ctx, directoryID, id)
		if err != nil {
			if errors.Is(err, ErrResourceMissing) {
				return organization.ErrGroupMemberInactive
			}
			return err
		}
		if row.PrincipalStatus != "active" || row.MembershipStatus != "active" {
			return organization.ErrGroupMemberInactive
		}
	}
	return nil
}

func userResource(record userRecord) UserResource {
	emails := make([]UserEmail, 0, 1)
	if record.Email != "" {
		emails = append(emails, UserEmail{Value: record.Email, Type: "work", Primary: true})
	}
	return UserResource{
		Schemas: []string{UserSchema}, ID: record.ResourceID, ExternalID: record.ExternalID,
		UserName: record.LoginName, Name: UserName{Formatted: record.DisplayName}, DisplayName: record.DisplayName,
		Active: record.PrincipalStatus == "active" && record.MembershipStatus == "active", Emails: emails,
		Meta: ResourceMeta{ResourceType: ResourceUser, Created: unixTime(record.CreatedAt), LastModified: unixTime(record.UpdatedAt), Location: "/scim/v2/Users/" + record.ResourceID, Version: weakETag(record.Version)},
	}
}

func groupResource(record groupRecord, ids []string) GroupResource {
	members := make([]SCIMGroupMember, 0, len(ids))
	for _, id := range ids {
		members = append(members, SCIMGroupMember{Value: id, Ref: "/scim/v2/Users/" + id})
	}
	return GroupResource{
		Schemas: []string{GroupSchema}, ID: record.ResourceID, ExternalID: record.ExternalID, DisplayName: record.Name, Members: members,
		Meta: ResourceMeta{ResourceType: ResourceGroup, Created: unixTime(record.CreatedAt), LastModified: unixTime(record.UpdatedAt), Location: "/scim/v2/Groups/" + record.ResourceID, Version: weakETag(record.Version)},
	}
}

func unixTime(value int64) time.Time { return time.UnixMilli(value).UTC() }
