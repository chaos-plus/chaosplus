package governance

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/chaos-plus/chaosplus/internal/core/extension/auditx"
	"github.com/chaos-plus/chaosplus/internal/core/extension/policyx"
	"github.com/uptrace/bun"
)

const (
	ReviewStatusOpen      = "open"
	ReviewStatusCompleted = "completed"
	ReviewStatusCancelled = "cancelled"
	ReviewStatusExpired   = "expired"

	ReviewDecisionPending = "pending"
	ReviewDecisionKeep    = "keep"
	ReviewDecisionRevoke  = "revoke"

	ReviewGrantPermanent = "permanent"
	ReviewGrantTemporary = "temporary"

	maxReviewDuration = 365 * 24 * time.Hour
)

var (
	ErrInvalidReview           = errors.New("invalid access review")
	ErrReviewNotFound          = errors.New("access review not found")
	ErrReviewItemNotFound      = errors.New("access review item not found")
	ErrReviewEmpty             = errors.New("access review has no eligible grants")
	ErrReviewStateConflict     = errors.New("access review state conflict")
	ErrReviewIncomplete        = errors.New("access review has pending items")
	ErrReviewExpired           = errors.New("access review expired")
	ErrReviewSelfDecision      = errors.New("reviewer cannot decide their own access")
	ErrReviewLastAdministrator = errors.New("review cannot revoke the last tenant administrator")
)

type AccessReview struct {
	ID          string             `json:"id"`
	TenantID    string             `json:"tenant_id"`
	Name        string             `json:"name"`
	OwnerID     string             `json:"owner_id"`
	Status      string             `json:"status"`
	DueAt       time.Time          `json:"due_at"`
	CompletedAt *time.Time         `json:"completed_at,omitempty"`
	CreatedAt   time.Time          `json:"created_at"`
	UpdatedAt   time.Time          `json:"updated_at"`
	Total       int                `json:"total"`
	Pending     int                `json:"pending"`
	Kept        int                `json:"kept"`
	Revoked     int                `json:"revoked"`
	Items       []AccessReviewItem `json:"items"`
}

type AccessReviewItem struct {
	ID             string     `json:"id"`
	ReviewID       string     `json:"review_id"`
	PrincipalID    string     `json:"principal_id"`
	PrincipalName  string     `json:"principal_name"`
	RoleID         string     `json:"role_id"`
	RoleName       string     `json:"role_name"`
	GrantType      string     `json:"grant_type"`
	GrantID        string     `json:"grant_id,omitempty"`
	GrantCreatedAt time.Time  `json:"grant_created_at"`
	GrantExpiresAt *time.Time `json:"grant_expires_at,omitempty"`
	Decision       string     `json:"decision"`
	DecidedBy      string     `json:"decided_by,omitempty"`
	DecisionNote   string     `json:"decision_note,omitempty"`
	DecidedAt      *time.Time `json:"decided_at,omitempty"`
}

type CreateAccessReview struct {
	Name  string
	DueAt time.Time
}

type accessReviewRow struct {
	bun.BaseModel `bun:"table:iam_access_reviews"`
	TenantID      string `bun:"tenant_id,pk"`
	ID            string `bun:"id,pk"`
	Name          string `bun:"name"`
	OwnerID       string `bun:"owner_id"`
	Status        string `bun:"status"`
	DueAt         int64  `bun:"due_at"`
	CompletedAt   int64  `bun:"completed_at"`
	CreatedAt     int64  `bun:"created_at"`
	UpdatedAt     int64  `bun:"updated_at"`
}

type accessReviewItemRow struct {
	bun.BaseModel  `bun:"table:iam_access_review_items"`
	TenantID       string `bun:"tenant_id,pk"`
	ReviewID       string `bun:"review_id,pk"`
	ID             string `bun:"id,pk"`
	PrincipalID    string `bun:"principal_id"`
	PrincipalName  string `bun:"principal_name"`
	RoleID         string `bun:"role_id"`
	RoleName       string `bun:"role_name"`
	GrantType      string `bun:"grant_type"`
	GrantID        string `bun:"grant_id"`
	GrantCreatedAt int64  `bun:"grant_created_at"`
	GrantExpiresAt int64  `bun:"grant_expires_at"`
	Decision       string `bun:"decision"`
	DecidedBy      string `bun:"decided_by"`
	DecisionNote   string `bun:"decision_note"`
	DecidedAt      int64  `bun:"decided_at"`
}

type reviewableGrantRow struct {
	PrincipalID    string `bun:"principal_id"`
	PrincipalName  string `bun:"principal_name"`
	RoleID         string `bun:"role_id"`
	RoleName       string `bun:"role_name"`
	GrantType      string `bun:"grant_type"`
	GrantID        string `bun:"grant_id"`
	GrantCreatedAt int64  `bun:"grant_created_at"`
	GrantExpiresAt int64  `bun:"grant_expires_at"`
}

type accessReviewCountRow struct {
	ReviewID string `bun:"review_id"`
	Total    int    `bun:"total"`
	Pending  int    `bun:"pending"`
	Kept     int    `bun:"kept"`
	Revoked  int    `bun:"revoked"`
}

func (s *Service) CreateReview(ctx context.Context, tenantID, ownerID string, input CreateAccessReview) (AccessReview, error) {
	tenantID, ownerID, input.Name = strings.TrimSpace(tenantID), strings.TrimSpace(ownerID), strings.TrimSpace(input.Name)
	now := s.now().UTC()
	if !validID(tenantID, 128) || !validID(ownerID, 255) || len(input.Name) < 3 || len(input.Name) > 128 ||
		!input.DueAt.After(now) || input.DueAt.After(now.Add(maxReviewDuration)) {
		return AccessReview{}, ErrInvalidReview
	}
	reviewID, err := s.nextID()
	if err != nil || !validID(reviewID, 64) {
		return AccessReview{}, fmt.Errorf("generate access review id: %w", errors.Join(err, ErrInvalidReview))
	}
	row := accessReviewRow{
		TenantID: tenantID, ID: reviewID, Name: input.Name, OwnerID: ownerID, Status: ReviewStatusOpen,
		DueAt: input.DueAt.UTC().UnixMilli(), CreatedAt: now.UnixMilli(), UpdatedAt: now.UnixMilli(),
	}
	items := make([]accessReviewItemRow, 0)
	err = s.db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		active, err := activeTenantMember(ctx, tx, tenantID, ownerID)
		if err != nil {
			return err
		}
		if !active {
			return ErrRequesterInactive
		}
		grants, err := listReviewableGrants(ctx, tx, tenantID, now.UnixMilli())
		if err != nil {
			return err
		}
		if len(grants) == 0 {
			return ErrReviewEmpty
		}
		if _, err := tx.NewInsert().Model(&row).Exec(ctx); err != nil {
			return fmt.Errorf("insert access review: %w", err)
		}
		for _, grant := range grants {
			itemID, err := s.nextID()
			if err != nil || !validID(itemID, 64) {
				return fmt.Errorf("generate access review item id: %w", errors.Join(err, ErrInvalidReview))
			}
			items = append(items, accessReviewItemRow{
				TenantID: tenantID, ReviewID: reviewID, ID: itemID,
				PrincipalID: grant.PrincipalID, PrincipalName: grant.PrincipalName,
				RoleID: grant.RoleID, RoleName: grant.RoleName, GrantType: grant.GrantType, GrantID: grant.GrantID,
				GrantCreatedAt: grant.GrantCreatedAt, GrantExpiresAt: grant.GrantExpiresAt, Decision: ReviewDecisionPending,
			})
		}
		if _, err := tx.NewInsert().Model(&items).Exec(ctx); err != nil {
			return fmt.Errorf("insert access review items: %w", err)
		}
		event := auditx.NewEvent(ctx, tenantID, "access_review_created", "access_review", reviewID)
		event.Detail["owner_id"] = ownerID
		event.Detail["item_count"] = len(items)
		event.Detail["due_at"] = input.DueAt.UTC().Format(time.RFC3339Nano)
		return s.audit(ctx, tx, event)
	})
	if err != nil {
		return AccessReview{}, fmt.Errorf("create access review: %w", err)
	}
	return reviewFromRows(row, items, now), nil
}

func (s *Service) ListReviews(ctx context.Context, tenantID string) ([]AccessReview, error) {
	if !validID(tenantID, 128) {
		return nil, ErrInvalidReview
	}
	rows := make([]accessReviewRow, 0)
	if err := s.db.NewSelect().Model(&rows).Where("tenant_id = ?", tenantID).Order("created_at DESC", "id DESC").Limit(100).Scan(ctx); err != nil {
		return nil, fmt.Errorf("list access reviews: %w", err)
	}
	counts, err := reviewCounts(ctx, s.db, tenantID)
	if err != nil {
		return nil, err
	}
	now := s.now().UTC()
	reviews := make([]AccessReview, 0, len(rows))
	for _, row := range rows {
		review := reviewFromRows(row, nil, now)
		if count, ok := counts[row.ID]; ok {
			review.Total, review.Pending, review.Kept, review.Revoked = count.Total, count.Pending, count.Kept, count.Revoked
		}
		reviews = append(reviews, review)
	}
	return reviews, nil
}

func (s *Service) GetReview(ctx context.Context, tenantID, reviewID string) (AccessReview, error) {
	if !validID(tenantID, 128) || !validID(reviewID, 64) {
		return AccessReview{}, ErrInvalidReview
	}
	row, err := getReviewRow(ctx, s.db, tenantID, reviewID)
	if err != nil {
		return AccessReview{}, err
	}
	items := make([]accessReviewItemRow, 0)
	if err := s.db.NewSelect().Model(&items).Where("tenant_id = ? AND review_id = ?", tenantID, reviewID).
		Order("principal_name ASC", "principal_id ASC", "role_name ASC", "role_id ASC", "grant_type ASC").Scan(ctx); err != nil {
		return AccessReview{}, fmt.Errorf("list access review items: %w", err)
	}
	return reviewFromRows(row, items, s.now().UTC()), nil
}

func (s *Service) DecideReviewItem(ctx context.Context, tenantID, reviewID, itemID, actorID, decision, note string) (AccessReviewItem, error) {
	tenantID, reviewID, itemID, actorID = strings.TrimSpace(tenantID), strings.TrimSpace(reviewID), strings.TrimSpace(itemID), strings.TrimSpace(actorID)
	note = strings.TrimSpace(note)
	if !validID(tenantID, 128) || !validID(reviewID, 64) || !validID(itemID, 64) || !validID(actorID, 255) ||
		(decision != ReviewDecisionKeep && decision != ReviewDecisionRevoke) || len(note) > 500 {
		return AccessReviewItem{}, ErrInvalidReview
	}
	now := s.now().UTC()
	var updated accessReviewItemRow
	err := s.db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		review, err := getReviewRow(ctx, tx, tenantID, reviewID)
		if err != nil {
			return err
		}
		if review.Status != ReviewStatusOpen {
			return ErrReviewStateConflict
		}
		if review.DueAt <= now.UnixMilli() {
			return ErrReviewExpired
		}
		item, err := getReviewItemRow(ctx, tx, tenantID, reviewID, itemID)
		if err != nil {
			return err
		}
		if item.Decision != ReviewDecisionPending {
			return ErrReviewStateConflict
		}
		if item.PrincipalID == actorID {
			return ErrReviewSelfDecision
		}
		policyChanged := false
		if decision == ReviewDecisionRevoke {
			switch item.GrantType {
			case ReviewGrantPermanent:
				policyChanged, err = s.grants.RemovePermanent(ctx, tx, tenantID, item.RoleID, item.PrincipalID, item.GrantCreatedAt)
			case ReviewGrantTemporary:
				if err = policyx.Lock(ctx, tx, s.dialect, tenantID); err == nil {
					policyChanged, err = s.grants.Revoke(ctx, tx, tenantID, item.GrantID)
				}
			default:
				err = ErrInvalidReview
			}
			if err != nil {
				return err
			}
		}
		result, err := tx.NewUpdate().Model((*accessReviewItemRow)(nil)).Set("decision = ?", decision).
			Set("decided_by = ?", actorID).Set("decision_note = ?", note).Set("decided_at = ?", now.UnixMilli()).
			Where("tenant_id = ? AND review_id = ? AND id = ? AND decision = ?", tenantID, reviewID, itemID, ReviewDecisionPending).Exec(ctx)
		if err != nil {
			return fmt.Errorf("decide access review item: %w", err)
		}
		if affected, _ := result.RowsAffected(); affected != 1 {
			return ErrReviewStateConflict
		}
		if _, err := tx.NewUpdate().Model((*accessReviewRow)(nil)).Set("updated_at = ?", now.UnixMilli()).
			Where("tenant_id = ? AND id = ? AND status = ?", tenantID, reviewID, ReviewStatusOpen).Exec(ctx); err != nil {
			return fmt.Errorf("touch access review: %w", err)
		}
		if policyChanged {
			if err := policyx.Advance(ctx, tx, s.dialect, tenantID, now.UnixMilli()); err != nil {
				return err
			}
		}
		item.Decision, item.DecidedBy, item.DecisionNote, item.DecidedAt = decision, actorID, note, now.UnixMilli()
		updated = item
		eventType := "access_review_item_kept"
		if decision == ReviewDecisionRevoke {
			eventType = "access_review_item_revoked"
		}
		event := auditx.NewEvent(ctx, tenantID, eventType, "access_review_item", itemID)
		event.Detail["review_id"] = reviewID
		event.Detail["principal_id"] = item.PrincipalID
		event.Detail["role_id"] = item.RoleID
		event.Detail["grant_type"] = item.GrantType
		event.Detail["grant_changed"] = policyChanged
		return s.audit(ctx, tx, event)
	})
	if err != nil {
		return AccessReviewItem{}, fmt.Errorf("decide access review item: %w", err)
	}
	return reviewItemFromRow(updated), nil
}

func (s *Service) CompleteReview(ctx context.Context, tenantID, reviewID, actorID string) (AccessReview, error) {
	return s.finishReview(ctx, tenantID, reviewID, actorID, ReviewStatusCompleted)
}

func (s *Service) CancelReview(ctx context.Context, tenantID, reviewID, actorID string) (AccessReview, error) {
	return s.finishReview(ctx, tenantID, reviewID, actorID, ReviewStatusCancelled)
}

func (s *Service) finishReview(ctx context.Context, tenantID, reviewID, actorID, status string) (AccessReview, error) {
	tenantID, reviewID, actorID = strings.TrimSpace(tenantID), strings.TrimSpace(reviewID), strings.TrimSpace(actorID)
	if !validID(tenantID, 128) || !validID(reviewID, 64) || !validID(actorID, 255) ||
		(status != ReviewStatusCompleted && status != ReviewStatusCancelled) {
		return AccessReview{}, ErrInvalidReview
	}
	now := s.now().UTC()
	var updated AccessReview
	err := s.db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		row, err := getReviewRow(ctx, tx, tenantID, reviewID)
		if err != nil {
			return err
		}
		if row.Status != ReviewStatusOpen {
			return ErrReviewStateConflict
		}
		if row.DueAt <= now.UnixMilli() {
			return ErrReviewExpired
		}
		if status == ReviewStatusCompleted {
			pending, err := tx.NewSelect().Model((*accessReviewItemRow)(nil)).Where("tenant_id = ? AND review_id = ? AND decision = ?", tenantID, reviewID, ReviewDecisionPending).Count(ctx)
			if err != nil {
				return fmt.Errorf("count pending access review items: %w", err)
			}
			if pending > 0 {
				return ErrReviewIncomplete
			}
		}
		completedAt := int64(0)
		if status == ReviewStatusCompleted {
			completedAt = now.UnixMilli()
		}
		result, err := tx.NewUpdate().Model((*accessReviewRow)(nil)).Set("status = ?", status).
			Set("completed_at = ?", completedAt).Set("updated_at = ?", now.UnixMilli()).
			Where("tenant_id = ? AND id = ? AND status = ?", tenantID, reviewID, ReviewStatusOpen).Exec(ctx)
		if err != nil {
			return fmt.Errorf("finish access review: %w", err)
		}
		if affected, _ := result.RowsAffected(); affected != 1 {
			return ErrReviewStateConflict
		}
		row.Status, row.CompletedAt, row.UpdatedAt = status, completedAt, now.UnixMilli()
		items := make([]accessReviewItemRow, 0)
		if err := tx.NewSelect().Model(&items).Where("tenant_id = ? AND review_id = ?", tenantID, reviewID).
			Order("principal_name ASC", "principal_id ASC", "role_name ASC", "role_id ASC", "grant_type ASC").Scan(ctx); err != nil {
			return fmt.Errorf("list finished access review items: %w", err)
		}
		updated = reviewFromRows(row, items, now)
		event := auditx.NewEvent(ctx, tenantID, "access_review_"+status, "access_review", reviewID)
		event.Detail["actor_id"] = actorID
		return s.audit(ctx, tx, event)
	})
	if err != nil {
		return AccessReview{}, fmt.Errorf("finish access review: %w", err)
	}
	return updated, nil
}

func listReviewableGrants(ctx context.Context, db bun.IDB, tenantID string, now int64) ([]reviewableGrantRow, error) {
	rows := make([]reviewableGrantRow, 0)
	err := db.NewRaw(`
SELECT members.user_subject AS principal_id, tenant_members.display_name AS principal_name,
       roles.id AS role_id, roles.name AS role_name, 'permanent' AS grant_type, '' AS grant_id,
       members.created_at AS grant_created_at, 0 AS grant_expires_at
FROM iam_role_members AS members
JOIN iam_roles AS roles ON roles.tenant_id = members.tenant_id AND roles.id = members.role_id
JOIN iam_tenant_members AS tenant_members
  ON tenant_members.tenant_id = members.tenant_id AND tenant_members.user_subject = members.user_subject AND tenant_members.status = 'active'
WHERE members.tenant_id = ?
UNION ALL
SELECT grants.principal_id, tenant_members.display_name, roles.id, roles.name, 'temporary', grants.id,
       grants.created_at, grants.ends_at
FROM iam_temporary_role_grants AS grants
JOIN iam_roles AS roles ON roles.tenant_id = grants.tenant_id AND roles.id = grants.role_id
JOIN iam_tenant_members AS tenant_members
  ON tenant_members.tenant_id = grants.tenant_id AND tenant_members.user_subject = grants.principal_id AND tenant_members.status = 'active'
WHERE grants.tenant_id = ? AND grants.starts_at <= ? AND grants.ends_at > ?
ORDER BY principal_id, role_id, grant_type, grant_id`, tenantID, tenantID, now, now).Scan(ctx, &rows)
	if err != nil {
		return nil, fmt.Errorf("list reviewable role grants: %w", err)
	}
	return rows, nil
}

func getReviewRow(ctx context.Context, db bun.IDB, tenantID, reviewID string) (accessReviewRow, error) {
	var row accessReviewRow
	if err := db.NewSelect().Model(&row).Where("tenant_id = ? AND id = ?", tenantID, reviewID).Scan(ctx); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return row, ErrReviewNotFound
		}
		return row, fmt.Errorf("get access review: %w", err)
	}
	return row, nil
}

func getReviewItemRow(ctx context.Context, db bun.IDB, tenantID, reviewID, itemID string) (accessReviewItemRow, error) {
	var row accessReviewItemRow
	if err := db.NewSelect().Model(&row).Where("tenant_id = ? AND review_id = ? AND id = ?", tenantID, reviewID, itemID).Scan(ctx); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return row, ErrReviewItemNotFound
		}
		return row, fmt.Errorf("get access review item: %w", err)
	}
	return row, nil
}

func reviewCounts(ctx context.Context, db bun.IDB, tenantID string) (map[string]accessReviewCountRow, error) {
	rows := make([]accessReviewCountRow, 0)
	if err := db.NewRaw(`SELECT review_id, COUNT(*) AS total,
        SUM(CASE WHEN decision = 'pending' THEN 1 ELSE 0 END) AS pending,
        SUM(CASE WHEN decision = 'keep' THEN 1 ELSE 0 END) AS kept,
        SUM(CASE WHEN decision = 'revoke' THEN 1 ELSE 0 END) AS revoked
      FROM iam_access_review_items WHERE tenant_id = ? GROUP BY review_id`, tenantID).Scan(ctx, &rows); err != nil {
		return nil, fmt.Errorf("count access review items: %w", err)
	}
	result := make(map[string]accessReviewCountRow, len(rows))
	for _, row := range rows {
		result[row.ReviewID] = row
	}
	return result, nil
}

func reviewFromRows(row accessReviewRow, rows []accessReviewItemRow, now time.Time) AccessReview {
	status := row.Status
	if status == ReviewStatusOpen && row.DueAt <= now.UnixMilli() {
		status = ReviewStatusExpired
	}
	items := make([]AccessReviewItem, 0, len(rows))
	review := AccessReview{
		ID: row.ID, TenantID: row.TenantID, Name: row.Name, OwnerID: row.OwnerID, Status: status,
		DueAt: time.UnixMilli(row.DueAt).UTC(), CreatedAt: time.UnixMilli(row.CreatedAt).UTC(), UpdatedAt: time.UnixMilli(row.UpdatedAt).UTC(), Items: items,
	}
	if row.CompletedAt > 0 {
		value := time.UnixMilli(row.CompletedAt).UTC()
		review.CompletedAt = &value
	}
	for _, itemRow := range rows {
		item := reviewItemFromRow(itemRow)
		review.Items = append(review.Items, item)
		review.Total++
		switch item.Decision {
		case ReviewDecisionPending:
			review.Pending++
		case ReviewDecisionKeep:
			review.Kept++
		case ReviewDecisionRevoke:
			review.Revoked++
		}
	}
	return review
}

func reviewItemFromRow(row accessReviewItemRow) AccessReviewItem {
	item := AccessReviewItem{
		ID: row.ID, ReviewID: row.ReviewID, PrincipalID: row.PrincipalID, PrincipalName: row.PrincipalName,
		RoleID: row.RoleID, RoleName: row.RoleName, GrantType: row.GrantType, GrantID: row.GrantID,
		GrantCreatedAt: time.UnixMilli(row.GrantCreatedAt).UTC(), Decision: row.Decision,
		DecidedBy: row.DecidedBy, DecisionNote: row.DecisionNote,
	}
	if row.GrantExpiresAt > 0 {
		value := time.UnixMilli(row.GrantExpiresAt).UTC()
		item.GrantExpiresAt = &value
	}
	if row.DecidedAt > 0 {
		value := time.UnixMilli(row.DecidedAt).UTC()
		item.DecidedAt = &value
	}
	return item
}
