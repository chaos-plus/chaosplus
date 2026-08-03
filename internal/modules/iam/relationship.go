package iam

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/chaos-plus/chaosplus/internal/core/extension/authz"
	"github.com/chaos-plus/chaosplus/internal/core/extension/policyx"
	iamdomain "github.com/chaos-plus/chaosplus/internal/modules/iam/domain"
	"github.com/uptrace/bun"
)

const (
	defaultRelationshipDepth = 8
	maxRelationshipDepth     = 16
	relationshipQueryBatch   = 200
)

type relationshipRow struct {
	bun.BaseModel   `bun:"table:iam_relationships"`
	TenantID        string `bun:"tenant_id,pk"`
	SubjectType     string `bun:"subject_type,pk"`
	SubjectID       string `bun:"subject_id,pk"`
	SubjectRelation string `bun:"subject_relation,pk"`
	Relation        string `bun:"relation,pk"`
	ResourceType    string `bun:"resource_type,pk"`
	ResourceID      string `bun:"resource_id,pk"`
	StartsAt        int64
	EndsAt          int64
	ConditionJSON   string
	CreatedAt       int64
}

type resourceRelationshipRow struct {
	bun.BaseModel   `bun:"table:iam_resource_relationships"`
	TenantID        string `bun:"tenant_id,pk"`
	EntityID        string `bun:"entity_id,pk"`
	SubjectType     string `bun:"subject_type,pk"`
	SubjectID       string `bun:"subject_id,pk"`
	SubjectRelation string `bun:"subject_relation,pk"`
	Relation        string `bun:"relation,pk"`
	ResourceType    string `bun:"resource_type,pk"`
	ResourceID      string `bun:"resource_id,pk"`
	StartsAt        int64
	EndsAt          int64
	ConditionJSON   string
	CreatedAt       int64
}

type relationshipNode struct {
	Type     string
	ID       string
	Relation string
}

type relationshipGrant struct {
	EntityID     string
	ResourceType string
	ResourceID   string
	Relation     string
	Path         []authz.RelationshipStep
}

func (r *Repository) ListRelationships(ctx context.Context, tenantID string, filter iamdomain.RelationshipFilter) ([]iamdomain.Relationship, error) {
	if filter.EntityID != "" {
		rows := make([]resourceRelationshipRow, 0)
		query := r.executor.NewSelect().Model(&rows).Where("tenant_id = ? AND entity_id = ?", tenantID, filter.EntityID)
		if filter.ResourceType != "" {
			query = query.Where("resource_type = ?", filter.ResourceType)
		}
		if filter.ResourceID != "" {
			query = query.Where("resource_id = ?", filter.ResourceID)
		}
		if err := query.Order("resource_type ASC", "resource_id ASC", "relation ASC", "subject_type ASC", "subject_id ASC", "subject_relation ASC").Scan(ctx); err != nil {
			return nil, fmt.Errorf("list IAM resource relationships: %w", err)
		}
		result := make([]iamdomain.Relationship, 0, len(rows))
		for _, row := range rows {
			result = append(result, resourceRelationshipFromRow(row))
		}
		return result, nil
	}
	rows := make([]relationshipRow, 0)
	query := r.executor.NewSelect().Model(&rows).Where("tenant_id = ?", tenantID)
	if filter.ResourceType != "" {
		query = query.Where("resource_type = ?", filter.ResourceType)
	}
	if filter.ResourceID != "" {
		query = query.Where("resource_id = ?", filter.ResourceID)
	}
	if err := query.Order("resource_type ASC", "resource_id ASC", "relation ASC", "subject_type ASC", "subject_id ASC", "subject_relation ASC").Scan(ctx); err != nil {
		return nil, fmt.Errorf("list IAM relationships: %w", err)
	}
	result := make([]iamdomain.Relationship, 0, len(rows))
	for _, row := range rows {
		result = append(result, relationshipFromRow(row))
	}
	return result, nil
}

func (r *Repository) PutRelationship(ctx context.Context, relationship iamdomain.Relationship) (iamdomain.Relationship, bool, error) {
	if relationship.EntityID != "" {
		row := resourceRelationshipToRow(relationship)
		var current resourceRelationshipRow
		err := r.executor.NewSelect().Model(&current).
			Where("tenant_id = ? AND entity_id = ? AND subject_type = ? AND subject_id = ? AND subject_relation = ? AND relation = ? AND resource_type = ? AND resource_id = ?",
				row.TenantID, row.EntityID, row.SubjectType, row.SubjectID, row.SubjectRelation, row.Relation, row.ResourceType, row.ResourceID).Scan(ctx)
		if err == nil {
			if current.StartsAt == row.StartsAt && current.EndsAt == row.EndsAt && current.ConditionJSON == row.ConditionJSON {
				return resourceRelationshipFromRow(current), false, nil
			}
			_, err = r.executor.NewUpdate().Model((*resourceRelationshipRow)(nil)).
				Set("starts_at = ?, ends_at = ?, condition_json = ?", row.StartsAt, row.EndsAt, row.ConditionJSON).
				Where("tenant_id = ? AND entity_id = ? AND subject_type = ? AND subject_id = ? AND subject_relation = ? AND relation = ? AND resource_type = ? AND resource_id = ?",
					row.TenantID, row.EntityID, row.SubjectType, row.SubjectID, row.SubjectRelation, row.Relation, row.ResourceType, row.ResourceID).Exec(ctx)
			if err != nil {
				return iamdomain.Relationship{}, false, fmt.Errorf("update IAM resource relationship window: %w", err)
			}
			row.CreatedAt = current.CreatedAt
			return resourceRelationshipFromRow(row), true, nil
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return iamdomain.Relationship{}, false, fmt.Errorf("get IAM resource relationship: %w", err)
		}
		if _, err := r.executor.NewInsert().Model(&row).Exec(ctx); err != nil {
			return iamdomain.Relationship{}, false, fmt.Errorf("insert IAM resource relationship: %w", err)
		}
		return resourceRelationshipFromRow(row), true, nil
	}
	row := relationshipToRow(relationship)
	var current relationshipRow
	err := r.executor.NewSelect().Model(&current).
		Where("tenant_id = ? AND subject_type = ? AND subject_id = ? AND subject_relation = ? AND relation = ? AND resource_type = ? AND resource_id = ?",
			row.TenantID, row.SubjectType, row.SubjectID, row.SubjectRelation, row.Relation, row.ResourceType, row.ResourceID).Scan(ctx)
	if err == nil {
		if current.StartsAt == row.StartsAt && current.EndsAt == row.EndsAt && current.ConditionJSON == row.ConditionJSON {
			return relationshipFromRow(current), false, nil
		}
		_, err = r.executor.NewUpdate().Model((*relationshipRow)(nil)).
			Set("starts_at = ?, ends_at = ?, condition_json = ?", row.StartsAt, row.EndsAt, row.ConditionJSON).
			Where("tenant_id = ? AND subject_type = ? AND subject_id = ? AND subject_relation = ? AND relation = ? AND resource_type = ? AND resource_id = ?",
				row.TenantID, row.SubjectType, row.SubjectID, row.SubjectRelation, row.Relation, row.ResourceType, row.ResourceID).Exec(ctx)
		if err != nil {
			return iamdomain.Relationship{}, false, fmt.Errorf("update IAM relationship window: %w", err)
		}
		row.CreatedAt = current.CreatedAt
		return relationshipFromRow(row), true, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return iamdomain.Relationship{}, false, fmt.Errorf("get IAM relationship: %w", err)
	}
	if _, err := r.executor.NewInsert().Model(&row).Exec(ctx); err != nil {
		return iamdomain.Relationship{}, false, fmt.Errorf("insert IAM relationship: %w", err)
	}
	return relationshipFromRow(row), true, nil
}

func (r *Repository) DeleteRelationship(ctx context.Context, relationship iamdomain.Relationship) (bool, error) {
	if relationship.EntityID != "" {
		result, err := r.executor.NewDelete().Model((*resourceRelationshipRow)(nil)).
			Where("tenant_id = ? AND entity_id = ? AND subject_type = ? AND subject_id = ? AND subject_relation = ? AND relation = ? AND resource_type = ? AND resource_id = ?",
				relationship.TenantID, relationship.EntityID, relationship.SubjectType, relationship.SubjectID, relationship.SubjectRelation,
				relationship.Relation, relationship.ResourceType, relationship.ResourceID).Exec(ctx)
		if err != nil {
			return false, fmt.Errorf("delete IAM resource relationship: %w", err)
		}
		affected, _ := result.RowsAffected()
		return affected > 0, nil
	}
	result, err := r.executor.NewDelete().Model((*relationshipRow)(nil)).
		Where("tenant_id = ? AND subject_type = ? AND subject_id = ? AND subject_relation = ? AND relation = ? AND resource_type = ? AND resource_id = ?",
			relationship.TenantID, relationship.SubjectType, relationship.SubjectID, relationship.SubjectRelation,
			relationship.Relation, relationship.ResourceType, relationship.ResourceID).Exec(ctx)
	if err != nil {
		return false, fmt.Errorf("delete IAM relationship: %w", err)
	}
	affected, _ := result.RowsAffected()
	return affected > 0, nil
}

func (r *Repository) entityRelationshipCount(ctx context.Context, tenantID, entityID string) (int, error) {
	count, err := r.executor.NewSelect().Model((*relationshipRow)(nil)).
		Where("tenant_id = ? AND (resource_id = ? OR (subject_type = 'entity' AND subject_id = ?))", tenantID, entityID, entityID).Count(ctx)
	if err != nil {
		return 0, fmt.Errorf("count entity relationships: %w", err)
	}
	resourceCount, err := r.executor.NewSelect().Model((*resourceRelationshipRow)(nil)).
		Where("tenant_id = ? AND (entity_id = ? OR (subject_type = 'entity' AND subject_id = ?))", tenantID, entityID, entityID).Count(ctx)
	if err != nil {
		return 0, fmt.Errorf("count entity resource relationships: %w", err)
	}
	return count + resourceCount, nil
}

func (s *Service) ListRelationships(ctx context.Context, tenantID string, filter iamdomain.RelationshipFilter) ([]iamdomain.Relationship, error) {
	filter.EntityID, filter.ResourceType, filter.ResourceID = strings.TrimSpace(filter.EntityID), strings.TrimSpace(filter.ResourceType), strings.TrimSpace(filter.ResourceID)
	if err := validateTenant(tenantID); err != nil || len(filter.EntityID) > 64 || (filter.ResourceType != "" && !validEntityType(filter.ResourceType)) || len(filter.ResourceID) > 255 || (filter.EntityID == "" && len(filter.ResourceID) > 64) {
		return nil, fmt.Errorf("%w: invalid relationship filter", iamdomain.ErrInvalidArgument)
	}
	return s.repo.ListRelationships(ctx, tenantID, filter)
}

func (s *Service) PutRelationship(ctx context.Context, relationship iamdomain.Relationship) (iamdomain.Relationship, bool, error) {
	normalizeRelationship(&relationship)
	condition, err := policyx.CanonicalCondition(relationship.Condition)
	if err != nil {
		return iamdomain.Relationship{}, false, fmt.Errorf("%w: %v", iamdomain.ErrInvalidRelationshipCondition, err)
	}
	relationship.Condition = condition
	if err := validateRelationshipShape(relationship); err != nil {
		return iamdomain.Relationship{}, false, err
	}
	if err := validateRelationshipWindow(relationship, s.repo.now().UTC()); err != nil {
		return iamdomain.Relationship{}, false, err
	}
	if relationship.EntityID != "" && !s.resourceRelationDeclared(relationship.ResourceType, relationship.Relation) {
		return iamdomain.Relationship{}, false, fmt.Errorf("%w: resource type does not declare this relation", iamdomain.ErrInvalidRelationship)
	}
	targetType := "entity"
	if relationship.EntityID != "" {
		targetType = relationship.ResourceType
	}
	record := newAuditRecord(ctx, relationship.TenantID, "relationship_put", targetType, relationship.ResourceID)
	record.Detail["subject_type"], record.Detail["subject_id"] = relationship.SubjectType, relationship.SubjectID
	record.Detail["subject_relation"], record.Detail["relation"] = relationship.SubjectRelation, relationship.Relation
	record.Detail["resource_type"] = relationship.ResourceType
	if relationship.EntityID != "" {
		record.Detail["entity_id"] = relationship.EntityID
	}
	if relationship.StartsAt != nil {
		record.Detail["starts_at"] = relationship.StartsAt.Format(time.RFC3339Nano)
	}
	if relationship.EndsAt != nil {
		record.Detail["ends_at"] = relationship.EndsAt.Format(time.RFC3339Nano)
	}
	if len(relationship.Condition) > 0 {
		record.Detail["condition"] = json.RawMessage(relationship.Condition)
	}
	var changed bool
	err = s.writes.Run(ctx, record, func(repo *Repository) error {
		if err := policyx.Lock(ctx, repo.executor, repo.dialect, relationship.TenantID); err != nil {
			return err
		}
		if err := validateRelationshipFacts(ctx, repo, relationship); err != nil {
			return err
		}
		relationship.CreatedAt = repo.now().UTC()
		var err error
		relationship, changed, err = repo.PutRelationship(ctx, relationship)
		if err != nil || !changed {
			record.SkipAudit = !changed
			return err
		}
		if relationship.EntityID == "" {
			rows, err := repo.ListRelationships(ctx, relationship.TenantID, iamdomain.RelationshipFilter{})
			if err != nil {
				return err
			}
			if !validRelationshipGraph(rows, maxRelationshipDepth) {
				return iamdomain.ErrRelationshipHierarchy
			}
		}
		record.PolicyChanged = true
		return nil
	})
	return relationship, changed, err
}

func (s *Service) DeleteRelationship(ctx context.Context, relationship iamdomain.Relationship) (bool, error) {
	normalizeRelationship(&relationship)
	if err := validateRelationshipShape(relationship); err != nil {
		return false, err
	}
	targetType := "entity"
	if relationship.EntityID != "" {
		targetType = relationship.ResourceType
	}
	record := newAuditRecord(ctx, relationship.TenantID, "relationship_deleted", targetType, relationship.ResourceID)
	record.Detail["subject_type"], record.Detail["subject_id"] = relationship.SubjectType, relationship.SubjectID
	record.Detail["subject_relation"], record.Detail["relation"] = relationship.SubjectRelation, relationship.Relation
	record.Detail["resource_type"] = relationship.ResourceType
	if relationship.EntityID != "" {
		record.Detail["entity_id"] = relationship.EntityID
	}
	var changed bool
	err := s.writes.Run(ctx, record, func(repo *Repository) error {
		var err error
		changed, err = repo.DeleteRelationship(ctx, relationship)
		record.PolicyChanged, record.SkipAudit = changed, !changed
		return err
	})
	return changed, err
}

func normalizeRelationship(relationship *iamdomain.Relationship) {
	if relationship == nil {
		return
	}
	relationship.TenantID = strings.TrimSpace(relationship.TenantID)
	relationship.EntityID = strings.TrimSpace(relationship.EntityID)
	relationship.SubjectType = strings.TrimSpace(relationship.SubjectType)
	relationship.SubjectID = strings.TrimSpace(relationship.SubjectID)
	relationship.SubjectRelation = strings.TrimSpace(relationship.SubjectRelation)
	relationship.Relation = strings.TrimSpace(relationship.Relation)
	relationship.ResourceType = strings.TrimSpace(relationship.ResourceType)
	relationship.ResourceID = strings.TrimSpace(relationship.ResourceID)
	relationship.StartsAt = normalizeRelationshipTime(relationship.StartsAt)
	relationship.EndsAt = normalizeRelationshipTime(relationship.EndsAt)
}

func normalizeRelationshipTime(value *time.Time) *time.Time {
	if value == nil {
		return nil
	}
	normalized := value.UTC().Truncate(time.Millisecond)
	return &normalized
}

func validateRelationshipWindow(relationship iamdomain.Relationship, now time.Time) error {
	if (relationship.StartsAt != nil && relationship.StartsAt.IsZero()) ||
		(relationship.EndsAt != nil && relationship.EndsAt.IsZero()) ||
		(relationship.StartsAt != nil && relationship.EndsAt != nil && !relationship.EndsAt.After(*relationship.StartsAt)) ||
		(relationship.EndsAt != nil && !relationship.EndsAt.After(now)) {
		return fmt.Errorf("%w: use a future end time after the optional start time", iamdomain.ErrInvalidRelationshipWindow)
	}
	return nil
}

func validateRelationshipShape(relationship iamdomain.Relationship) error {
	maxResourceID := 64
	if relationship.EntityID != "" {
		maxResourceID = 255
	}
	if validateTenant(relationship.TenantID) != nil || relationship.SubjectID == "" || len(relationship.SubjectID) > 255 ||
		len(relationship.EntityID) > 64 || !validEntityType(relationship.ResourceType) || relationship.ResourceID == "" || len(relationship.ResourceID) > maxResourceID || !validRelation(relationship.Relation) {
		return fmt.Errorf("%w: invalid subject, relation, or resource", iamdomain.ErrInvalidRelationship)
	}
	switch relationship.SubjectType {
	case "principal":
		if relationship.SubjectRelation != "" {
			return fmt.Errorf("%w: principal subjects cannot specify a subject relation", iamdomain.ErrInvalidRelationship)
		}
	case "group", "position":
		if relationship.SubjectRelation != "member" {
			return fmt.Errorf("%w: group and position subjects require the member relation", iamdomain.ErrInvalidRelationship)
		}
	case "entity":
		if !validRelation(relationship.SubjectRelation) {
			return fmt.Errorf("%w: entity subjects require owner, editor, or viewer", iamdomain.ErrInvalidRelationship)
		}
	default:
		return fmt.Errorf("%w: unknown subject type", iamdomain.ErrInvalidRelationship)
	}
	return nil
}

func validateRelationshipFacts(ctx context.Context, repo *Repository, relationship iamdomain.Relationship) error {
	resourceID := relationship.ResourceID
	if relationship.EntityID != "" {
		resourceID = relationship.EntityID
	}
	resource, err := repo.GetEntity(ctx, relationship.TenantID, resourceID)
	if err != nil {
		return err
	}
	if relationship.EntityID == "" && resource.Type != relationship.ResourceType {
		return fmt.Errorf("%w: resource type does not match the entity", iamdomain.ErrInvalidRelationship)
	}
	if resource.Status != iamdomain.EntityActive {
		return iamdomain.ErrRelationshipResourceInactive
	}
	switch relationship.SubjectType {
	case "principal":
		member, err := repo.GetMember(ctx, relationship.TenantID, relationship.SubjectID)
		if err != nil {
			if errors.Is(err, iamdomain.ErrMemberNotFound) {
				return iamdomain.ErrRelationshipSubjectMissing
			}
			return err
		}
		if member.Status != iamdomain.MemberActive {
			return iamdomain.ErrRelationshipSubjectInactive
		}
	case "entity":
		entity, err := repo.GetEntity(ctx, relationship.TenantID, relationship.SubjectID)
		if err != nil {
			if errors.Is(err, iamdomain.ErrEntityNotFound) {
				return iamdomain.ErrRelationshipSubjectMissing
			}
			return err
		}
		if entity.Status != iamdomain.EntityActive {
			return iamdomain.ErrRelationshipSubjectInactive
		}
	default:
		var status string
		table := "iam_groups"
		if relationship.SubjectType == "position" {
			table = "iam_positions"
		}
		err := repo.executor.NewSelect().Table(table).Column("status").Where("tenant_id = ? AND id = ?", relationship.TenantID, relationship.SubjectID).Scan(ctx, &status)
		if errors.Is(err, sql.ErrNoRows) {
			return iamdomain.ErrRelationshipSubjectMissing
		}
		if err != nil {
			return fmt.Errorf("get relationship subject: %w", err)
		}
		if status != "active" {
			return iamdomain.ErrRelationshipSubjectInactive
		}
	}
	return nil
}

func (s *Service) resourceRelationDeclared(resourceType, relation string) bool {
	for _, action := range s.registry.All() {
		index := sort.SearchStrings(action.AllowedRelations, relation)
		if action.Resource == resourceType && index < len(action.AllowedRelations) && action.AllowedRelations[index] == relation {
			return true
		}
	}
	return false
}

func validRelationshipGraph(relationships []iamdomain.Relationship, maxDepth int) bool {
	edges := make(map[relationshipNode][]relationshipNode)
	for _, relationship := range relationships {
		source := relationshipNode{Type: relationship.SubjectType, ID: relationship.SubjectID, Relation: relationship.SubjectRelation}
		target := relationshipNode{Type: "entity", ID: relationship.ResourceID, Relation: relationship.Relation}
		edges[source] = append(edges[source], target)
	}
	state := map[relationshipNode]uint8{}
	longest := map[relationshipNode]int{}
	var visit func(relationshipNode) (int, bool)
	visit = func(node relationshipNode) (int, bool) {
		if state[node] == 1 {
			return 0, false
		}
		if state[node] == 2 {
			return longest[node], true
		}
		state[node] = 1
		depth := 0
		for _, target := range edges[node] {
			childDepth, ok := visit(target)
			if !ok || childDepth+1 > maxDepth {
				return 0, false
			}
			depth = max(depth, childDepth+1)
		}
		state[node] = 2
		longest[node] = depth
		return depth, true
	}
	for node := range edges {
		if _, ok := visit(node); !ok {
			return false
		}
	}
	return true
}

func (a *Authorizer) loadRelationshipGrants(ctx context.Context, tenantID, subject string, trusted policyx.TrustedContext) ([]relationshipGrant, error) {
	now := trusted.Time.UTC().UnixMilli()
	frontier, err := a.relationshipRoots(ctx, tenantID, subject, now)
	if err != nil {
		return nil, err
	}
	visited := make(map[relationshipNode]bool, len(frontier))
	paths := make(map[relationshipNode][]authz.RelationshipStep)
	for _, node := range frontier {
		visited[node] = true
	}
	grants := make([]relationshipGrant, 0)
	for depth := 0; depth < defaultRelationshipDepth && len(frontier) > 0; depth++ {
		resourceRows, err := listActiveResourceRelationshipsBySubjects(ctx, a.db, tenantID, frontier, now)
		if err != nil {
			return nil, err
		}
		for _, row := range resourceRows {
			if !relationshipConditionMatches(row.ConditionJSON, trusted) {
				continue
			}
			source := relationshipNode{Type: row.SubjectType, ID: row.SubjectID, Relation: row.SubjectRelation}
			path := append([]authz.RelationshipStep(nil), paths[source]...)
			path = append(path, authz.RelationshipStep{
				EntityID: row.EntityID, SubjectType: row.SubjectType, SubjectID: row.SubjectID, SubjectRelation: row.SubjectRelation,
				Relation: row.Relation, ResourceType: row.ResourceType, ResourceID: row.ResourceID, Condition: relationshipCondition(row.ConditionJSON),
			})
			grants = append(grants, relationshipGrant{
				EntityID: row.EntityID, ResourceType: row.ResourceType, ResourceID: row.ResourceID, Relation: row.Relation, Path: path,
			})
		}
		rows, err := listActiveRelationshipsBySubjects(ctx, a.db, tenantID, frontier, now)
		if err != nil {
			return nil, err
		}
		next := make([]relationshipNode, 0, len(rows))
		for _, row := range rows {
			if !relationshipConditionMatches(row.ConditionJSON, trusted) {
				continue
			}
			source := relationshipNode{Type: row.SubjectType, ID: row.SubjectID, Relation: row.SubjectRelation}
			target := relationshipNode{Type: "entity", ID: row.ResourceID, Relation: row.Relation}
			if visited[target] {
				continue
			}
			path := append([]authz.RelationshipStep(nil), paths[source]...)
			path = append(path, authz.RelationshipStep{
				SubjectType: row.SubjectType, SubjectID: row.SubjectID, SubjectRelation: row.SubjectRelation,
				Relation: row.Relation, ResourceType: row.ResourceType, ResourceID: row.ResourceID, Condition: relationshipCondition(row.ConditionJSON),
			})
			visited[target], paths[target] = true, path
			grants = append(grants, relationshipGrant{ResourceType: row.ResourceType, ResourceID: row.ResourceID, Relation: row.Relation, Path: path})
			next = append(next, target)
		}
		frontier = next
	}
	return grants, nil
}

func (a *Authorizer) relationshipRoots(ctx context.Context, tenantID, subject string, now int64) ([]relationshipNode, error) {
	roots := []relationshipNode{{Type: "principal", ID: subject}}
	var groups []string
	if err := a.db.NewSelect().TableExpr("iam_group_members AS gm").ColumnExpr("gm.group_id").
		Join("JOIN iam_groups AS g ON g.tenant_id = gm.tenant_id AND g.id = gm.group_id AND g.status = 'active' AND g.group_type = 'static'").
		Where("gm.tenant_id = ? AND gm.principal_id = ? AND (gm.starts_at = 0 OR gm.starts_at <= ?) AND (gm.ends_at = 0 OR gm.ends_at > ?)", tenantID, subject, now, now).
		OrderExpr("gm.group_id ASC").Scan(ctx, &groups); err != nil {
		return nil, fmt.Errorf("list relationship groups: %w", err)
	}
	for _, id := range groups {
		roots = append(roots, relationshipNode{Type: "group", ID: id, Relation: "member"})
	}
	dynamicGroups, err := matchingDynamicGroupIDs(ctx, a.db, tenantID, subject)
	if err != nil {
		return nil, err
	}
	for _, id := range dynamicGroups {
		roots = append(roots, relationshipNode{Type: "group", ID: id, Relation: "member"})
	}
	var positions []string
	if err := a.db.NewSelect().TableExpr("iam_position_members AS pm").ColumnExpr("pm.position_id").
		Join("JOIN iam_positions AS p ON p.tenant_id = pm.tenant_id AND p.id = pm.position_id AND p.status = 'active'").
		Where("pm.tenant_id = ? AND pm.principal_id = ? AND (pm.starts_at = 0 OR pm.starts_at <= ?) AND (pm.ends_at = 0 OR pm.ends_at > ?)", tenantID, subject, now, now).
		OrderExpr("pm.position_id ASC").Scan(ctx, &positions); err != nil {
		return nil, fmt.Errorf("list relationship positions: %w", err)
	}
	for _, id := range positions {
		roots = append(roots, relationshipNode{Type: "position", ID: id, Relation: "member"})
	}
	return roots, nil
}

func listActiveRelationshipsBySubjects(ctx context.Context, db *bun.DB, tenantID string, subjects []relationshipNode, now int64) ([]relationshipRow, error) {
	rows := make([]relationshipRow, 0)
	for start := 0; start < len(subjects); start += relationshipQueryBatch {
		end := min(start+relationshipQueryBatch, len(subjects))
		var batch []relationshipRow
		query := db.NewSelect().TableExpr("iam_relationships AS rel").ColumnExpr("rel.*").
			Join("JOIN iam_entities AS resource ON resource.tenant_id = rel.tenant_id AND resource.id = rel.resource_id AND resource.type = rel.resource_type AND resource.status = 'active'").
			Where("rel.tenant_id = ? AND (rel.starts_at = 0 OR rel.starts_at <= ?) AND (rel.ends_at = 0 OR rel.ends_at > ?)", tenantID, now, now).
			WhereGroup(" AND ", func(q *bun.SelectQuery) *bun.SelectQuery {
				for _, subject := range subjects[start:end] {
					q = q.WhereOr("(rel.subject_type = ? AND rel.subject_id = ? AND rel.subject_relation = ?)", subject.Type, subject.ID, subject.Relation)
				}
				return q
			}).OrderExpr("rel.resource_type ASC, rel.resource_id ASC, rel.relation ASC, rel.subject_type ASC, rel.subject_id ASC")
		if err := query.Scan(ctx, &batch); err != nil {
			return nil, fmt.Errorf("traverse IAM relationships: %w", err)
		}
		rows = append(rows, batch...)
	}
	return rows, nil
}

func listActiveResourceRelationshipsBySubjects(ctx context.Context, db *bun.DB, tenantID string, subjects []relationshipNode, now int64) ([]resourceRelationshipRow, error) {
	rows := make([]resourceRelationshipRow, 0)
	for start := 0; start < len(subjects); start += relationshipQueryBatch {
		end := min(start+relationshipQueryBatch, len(subjects))
		var batch []resourceRelationshipRow
		query := db.NewSelect().TableExpr("iam_resource_relationships AS rel").ColumnExpr("rel.*").
			Join("JOIN iam_entities AS entity ON entity.tenant_id = rel.tenant_id AND entity.id = rel.entity_id AND entity.status = 'active'").
			Where("rel.tenant_id = ? AND (rel.starts_at = 0 OR rel.starts_at <= ?) AND (rel.ends_at = 0 OR rel.ends_at > ?)", tenantID, now, now).
			WhereGroup(" AND ", func(q *bun.SelectQuery) *bun.SelectQuery {
				for _, subject := range subjects[start:end] {
					q = q.WhereOr("(rel.subject_type = ? AND rel.subject_id = ? AND rel.subject_relation = ?)", subject.Type, subject.ID, subject.Relation)
				}
				return q
			}).OrderExpr("rel.entity_id ASC, rel.resource_type ASC, rel.resource_id ASC, rel.relation ASC, rel.subject_type ASC, rel.subject_id ASC")
		if err := query.Scan(ctx, &batch); err != nil {
			return nil, fmt.Errorf("traverse IAM resource relationships: %w", err)
		}
		rows = append(rows, batch...)
	}
	return rows, nil
}

func relationshipToRow(relationship iamdomain.Relationship) relationshipRow {
	return relationshipRow{
		TenantID: relationship.TenantID, SubjectType: relationship.SubjectType, SubjectID: relationship.SubjectID,
		SubjectRelation: relationship.SubjectRelation, Relation: relationship.Relation, ResourceType: relationship.ResourceType,
		ResourceID: relationship.ResourceID, StartsAt: optionalRelationshipUnixMilli(relationship.StartsAt),
		EndsAt: optionalRelationshipUnixMilli(relationship.EndsAt), ConditionJSON: string(relationship.Condition), CreatedAt: relationship.CreatedAt.UTC().UnixMilli(),
	}
}

func relationshipFromRow(row relationshipRow) iamdomain.Relationship {
	return iamdomain.Relationship{
		TenantID: row.TenantID, SubjectType: row.SubjectType, SubjectID: row.SubjectID, SubjectRelation: row.SubjectRelation,
		Relation: row.Relation, ResourceType: row.ResourceType, ResourceID: row.ResourceID,
		StartsAt: relationshipTime(row.StartsAt), EndsAt: relationshipTime(row.EndsAt), Condition: relationshipCondition(row.ConditionJSON), CreatedAt: time.UnixMilli(row.CreatedAt).UTC(),
	}
}

func resourceRelationshipToRow(relationship iamdomain.Relationship) resourceRelationshipRow {
	return resourceRelationshipRow{
		TenantID: relationship.TenantID, EntityID: relationship.EntityID, SubjectType: relationship.SubjectType,
		SubjectID: relationship.SubjectID, SubjectRelation: relationship.SubjectRelation, Relation: relationship.Relation,
		ResourceType: relationship.ResourceType, ResourceID: relationship.ResourceID, StartsAt: optionalRelationshipUnixMilli(relationship.StartsAt),
		EndsAt: optionalRelationshipUnixMilli(relationship.EndsAt), ConditionJSON: string(relationship.Condition), CreatedAt: relationship.CreatedAt.UTC().UnixMilli(),
	}
}

func resourceRelationshipFromRow(row resourceRelationshipRow) iamdomain.Relationship {
	return iamdomain.Relationship{
		TenantID: row.TenantID, EntityID: row.EntityID, SubjectType: row.SubjectType, SubjectID: row.SubjectID,
		SubjectRelation: row.SubjectRelation, Relation: row.Relation, ResourceType: row.ResourceType,
		ResourceID: row.ResourceID, StartsAt: relationshipTime(row.StartsAt), EndsAt: relationshipTime(row.EndsAt), Condition: relationshipCondition(row.ConditionJSON),
		CreatedAt: time.UnixMilli(row.CreatedAt).UTC(),
	}
}

func relationshipCondition(value string) json.RawMessage {
	if value == "" {
		return nil
	}
	return json.RawMessage(value)
}

func relationshipConditionMatches(value string, trusted policyx.TrustedContext) bool {
	matched, err := policyx.EvaluateCondition(relationshipCondition(value), trusted)
	return err == nil && matched
}

func optionalRelationshipUnixMilli(value *time.Time) int64 {
	if value == nil {
		return 0
	}
	return value.UTC().UnixMilli()
}

func relationshipTime(milliseconds int64) *time.Time {
	if milliseconds == 0 {
		return nil
	}
	value := time.UnixMilli(milliseconds).UTC()
	return &value
}

func validRelation(value string) bool {
	return value == "owner" || value == "editor" || value == "viewer"
}

func relationshipAllowed(action authz.Action, grant relationshipGrant) bool {
	if action.Resource != "entity" && action.Resource != grant.ResourceType {
		return false
	}
	index := sort.SearchStrings(action.AllowedRelations, grant.Relation)
	return index < len(action.AllowedRelations) && action.AllowedRelations[index] == grant.Relation
}
