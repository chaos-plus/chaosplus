package organization

import (
	"context"
	"fmt"
	"time"

	"github.com/chaos-plus/chaosplus/internal/core/extension/auditx"
	"github.com/chaos-plus/chaosplus/internal/core/extension/policyx"
	"github.com/chaos-plus/chaosplus/internal/infra/guid"
	"github.com/uptrace/bun"
)

type IDGenerator func() (guid.ID, error)

type Service struct {
	repo   *Repository
	audit  auditx.Appender
	nextID IDGenerator
	now    func() time.Time
}

func NewService(db *bun.DB, audit auditx.Appender, nextID IDGenerator) *Service {
	if db == nil || audit == nil || nextID == nil {
		panic("organization service requires database, audit appender, and id generator")
	}
	return &Service{repo: NewRepository(db), audit: audit, nextID: nextID, now: time.Now}
}

func (s *Service) List(ctx context.Context, tenantID guid.ID) ([]Department, error) {
	if tenantID.Zero() {
		return nil, ErrInvalid
	}
	rows, err := s.repo.list(ctx, tenantID)
	if err != nil {
		return nil, err
	}
	return orderDepartments(rows)
}

func (s *Service) Get(ctx context.Context, tenantID, id guid.ID) (Department, error) {
	if tenantID.Zero() || !validID(id) {
		return Department{}, ErrInvalid
	}
	row, err := s.repo.get(ctx, tenantID, id)
	if err != nil {
		return Department{}, err
	}
	depth, err := s.depth(ctx, tenantID, id)
	if err != nil {
		return Department{}, err
	}
	return departmentFromRow(row, depth), nil
}

func (s *Service) Create(ctx context.Context, tenantID guid.ID, input CreateDepartment) (Department, error) {
	tenantID, input, err := normalizeCreate(tenantID, input)
	if err != nil {
		return Department{}, err
	}
	id, err := s.nextID()
	if err != nil {
		return Department{}, fmt.Errorf("generate department id: %w", err)
	}
	if !validID(id) {
		return Department{}, fmt.Errorf("generate department id: %w", ErrInvalid)
	}
	now := s.now().UTC().UnixMilli()
	row := departmentRow{
		TenantID: tenantID, ID: id, ParentID: input.ParentID, Name: input.Name,
		NameKey: nameKey(input.Name), Status: input.Status, SortOrder: input.SortOrder,
		Version: 1, CreatedAt: now, UpdatedAt: now,
	}
	event := auditx.NewEvent(ctx, tenantID, "department_created", "department", id)
	event.Detail["status"] = input.Status
	event.Detail["has_parent"] = !input.ParentID.Zero()
	createdDepth := 0
	err = s.repo.db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		repo := s.repo.withExecutor(tx)
		if err := repo.insert(ctx, &row); err != nil {
			return err
		}
		ancestors, err := repo.ancestors(ctx, tenantID, id)
		if err != nil {
			return err
		}
		createdDepth = len(ancestors) - 1
		if err := policyx.Advance(ctx, tx, repo.dialect, tenantID, now); err != nil {
			return err
		}
		return s.audit(ctx, tx, event)
	})
	if err != nil {
		return Department{}, fmt.Errorf("create department: %w", err)
	}
	return departmentFromRow(row, createdDepth), nil
}

func (s *Service) Update(ctx context.Context, tenantID, id guid.ID, input UpdateDepartment) (Department, error) {
	tenantID, id, input, err := normalizeUpdate(tenantID, id, input)
	if err != nil {
		return Department{}, err
	}
	now := s.now().UTC().UnixMilli()
	var updated departmentRow
	updatedDepth := 0
	event := auditx.NewEvent(ctx, tenantID, "department_updated", "department", id)
	err = s.repo.db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		repo := s.repo.withExecutor(tx)
		current, err := repo.get(ctx, tenantID, id)
		if err != nil {
			return err
		}
		if current.Version != input.Version {
			return ErrVersionConflict
		}
		updated = current
		if input.ParentID != nil {
			updated.ParentID = *input.ParentID
		}
		if input.Name != nil {
			updated.Name, updated.NameKey = *input.Name, nameKey(*input.Name)
		}
		if input.Status != nil {
			updated.Status = *input.Status
		}
		if input.SortOrder != nil {
			updated.SortOrder = *input.SortOrder
		}
		if updated.ParentID != current.ParentID {
			if !updated.ParentID.Zero() {
				if _, err := repo.get(ctx, tenantID, updated.ParentID); err != nil {
					return err
				}
				cycle, err := repo.isDescendant(ctx, tenantID, id, updated.ParentID)
				if err != nil {
					return err
				}
				if cycle {
					return ErrHierarchyCycle
				}
			}
		}
		changed := updated.ParentID != current.ParentID || updated.Name != current.Name || updated.Status != current.Status || updated.SortOrder != current.SortOrder
		if !changed {
			ancestors, err := repo.ancestors(ctx, tenantID, id)
			if err != nil {
				return err
			}
			updatedDepth = len(ancestors) - 1
			return nil
		}
		updated.Version, updated.UpdatedAt = current.Version+1, now
		if err := repo.update(ctx, &updated, current.Version); err != nil {
			return err
		}
		if updated.ParentID != current.ParentID {
			if err := repo.move(ctx, tenantID, id, updated.ParentID); err != nil {
				return err
			}
		}
		ancestors, err := repo.ancestors(ctx, tenantID, id)
		if err != nil {
			return err
		}
		updatedDepth = len(ancestors) - 1
		event.Detail["parent_changed"] = updated.ParentID != current.ParentID
		event.Detail["name_changed"] = updated.Name != current.Name
		event.Detail["status_changed"] = updated.Status != current.Status
		event.Detail["sort_order_changed"] = updated.SortOrder != current.SortOrder
		if err := policyx.Advance(ctx, tx, repo.dialect, tenantID, now); err != nil {
			return err
		}
		return s.audit(ctx, tx, event)
	})
	if err != nil {
		return Department{}, fmt.Errorf("update department: %w", err)
	}
	return departmentFromRow(updated, updatedDepth), nil
}

func (s *Service) Delete(ctx context.Context, tenantID, id guid.ID, version int64) error {
	if tenantID.Zero() || !validID(id) || version < 1 {
		return ErrInvalid
	}
	now := s.now().UTC().UnixMilli()
	event := auditx.NewEvent(ctx, tenantID, "department_deleted", "department", id)
	err := s.repo.db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		repo := s.repo.withExecutor(tx)
		current, err := repo.get(ctx, tenantID, id)
		if err != nil {
			return err
		}
		if current.Version != version {
			return ErrVersionConflict
		}
		if err := repo.delete(ctx, tenantID, id, version); err != nil {
			return err
		}
		event.Detail["had_parent"] = !current.ParentID.Zero()
		if err := policyx.Advance(ctx, tx, repo.dialect, tenantID, now); err != nil {
			return err
		}
		return s.audit(ctx, tx, event)
	})
	if err != nil {
		return fmt.Errorf("delete department: %w", err)
	}
	return nil
}

func (s *Service) depth(ctx context.Context, tenantID, id guid.ID) (int, error) {
	ancestors, err := s.repo.ancestors(ctx, tenantID, id)
	if err != nil {
		return 0, err
	}
	if len(ancestors) == 0 {
		return 0, ErrNotFound
	}
	return len(ancestors) - 1, nil
}

func unixTime(milliseconds int64) time.Time { return time.UnixMilli(milliseconds).UTC() }
