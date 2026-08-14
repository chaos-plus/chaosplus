package machine

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/chaos-plus/chaosplus/internal/core/extension/authn"
	"github.com/chaos-plus/chaosplus/internal/core/extension/bunx"
	coreid "github.com/chaos-plus/chaosplus/internal/infra/guid"
	"github.com/uptrace/bun"
)

// Repository is the persistence port owned by the machine module.
type Repository interface {
	ConnectionDirectory
	Ping(context.Context) error
	Upsert(context.Context, Machine) error
	UpdateEntity(context.Context, coreid.ID, coreid.ID) error
	List(context.Context) ([]Machine, error)
	ListAll(context.Context) ([]Machine, error)
	UpdateAddress(context.Context, coreid.ID, string) error
	TouchHeartbeat(context.Context, coreid.ID) error
	UpdateToken(context.Context, coreid.ID, string) error
	Delete(context.Context, coreid.ID) error
}

var (
	ErrRouteHeld  = errors.New("machine connection lease is held")
	ErrRouteStale = errors.New("machine connection lease is expired or fenced")
)

type connectionLeaseRow struct {
	bun.BaseModel `bun:"table:machine_connection_leases"`
	MachineID     coreid.ID `bun:"machine_id,pk"`
	TenantID      coreid.ID `bun:"tenant_id,notnull"`
	EntityID      coreid.ID `bun:"entity_id,notnull"`
	HolderID      coreid.ID `bun:"holder_id,notnull"`
	FencingToken  int64     `bun:"fencing_token,notnull"`
	ExpiresAt     int64     `bun:"expires_at,notnull"`
	Name          string    `bun:"name,notnull"`
	RuntimesJSON  string    `bun:"runtimes_json,notnull"`
	OS            string    `bun:"os,notnull"`
	Address       string    `bun:"address,notnull"`
	UpdatedAt     int64     `bun:"updated_at,notnull"`
}

type pendingTokenRow struct {
	bun.BaseModel `bun:"table:machine_onboarding_tokens"`
	MachineID     coreid.ID `bun:"machine_id,pk"`
	TenantID      coreid.ID `bun:"tenant_id,notnull"`
	EntityID      coreid.ID `bun:"entity_id,notnull"`
	OwnerID       coreid.ID `bun:"owner_id,notnull"`
	TokenHash     string    `bun:"token_hash,notnull"`
	ExpiresAt     int64     `bun:"expires_at,notnull"`
	CreatedAt     int64     `bun:"created_at,notnull"`
}

type BunRepository struct{ db *bun.DB }

func NewRepository(db *bun.DB) *BunRepository {
	if db == nil {
		panic("machine repository requires database")
	}
	return &BunRepository{db: db}
}

func (r *BunRepository) Ping(ctx context.Context) error { return r.db.PingContext(ctx) }

func (r *BunRepository) databaseNow(ctx context.Context, db bun.IDB) (int64, error) {
	var now int64
	if err := db.NewSelect().ColumnExpr(bunx.NowMillisExpr(r.db.Dialect().Name().String())).Scan(ctx, &now); err != nil {
		return 0, fmt.Errorf("read database clock: %w", err)
	}
	return now, nil
}

func (r *BunRepository) StorePendingToken(ctx context.Context, token PendingToken) error {
	claims, err := requireClaims(ctx)
	if err != nil {
		return err
	}
	if token.MachineID.Zero() || token.TokenHash == "" || len(token.TokenHash) != 64 || token.ExpiresAt <= 0 ||
		token.TenantID != claims.TenantID || token.EntityID != claims.EntityID || token.OwnerID != claims.PrincipalID {
		return fmt.Errorf("store pending machine token: invalid token scope")
	}
	row := pendingTokenRow{MachineID: token.MachineID, TenantID: token.TenantID, EntityID: token.EntityID, OwnerID: token.OwnerID, TokenHash: token.TokenHash, ExpiresAt: token.ExpiresAt, CreatedAt: token.CreatedAt}
	if _, err := r.db.NewInsert().Model(&row).Exec(ctx); err != nil {
		return fmt.Errorf("store pending machine token: %w", err)
	}
	return nil
}

func (r *BunRepository) FindToken(ctx context.Context, tokenHash string) (AccessToken, error) {
	if len(tokenHash) != 64 {
		return AccessToken{}, ErrTokenInvalid
	}
	var pending pendingTokenRow
	err := r.db.NewSelect().Model(&pending).Where("token_hash = ?", tokenHash).Scan(ctx)
	if err == nil {
		now, clockErr := r.databaseNow(ctx, r.db)
		if clockErr != nil {
			return AccessToken{}, clockErr
		}
		if pending.ExpiresAt <= now {
			return AccessToken{}, ErrTokenExpired
		}
		return AccessToken{MachineID: pending.MachineID, TenantID: pending.TenantID, EntityID: pending.EntityID, OwnerID: pending.OwnerID, ExpiresAt: time.UnixMilli(pending.ExpiresAt).UTC()}, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return AccessToken{}, fmt.Errorf("find pending machine token: %w", err)
	}
	var machine Machine
	err = r.db.NewSelect().Model(&machine).Where("token_hash = ? AND deleted_at = 0", tokenHash).Scan(ctx)
	if errors.Is(err, sql.ErrNoRows) {
		return AccessToken{}, ErrTokenInvalid
	}
	if err != nil {
		return AccessToken{}, fmt.Errorf("find confirmed machine token: %w", err)
	}
	return AccessToken{MachineID: machine.ID, TenantID: machine.TenantID, EntityID: machine.EntityID, OwnerID: machine.OwnerID, LongTerm: true}, nil
}

func (r *BunRepository) PendingTokenForMachine(ctx context.Context, machineID coreid.ID) (PendingToken, error) {
	claims, err := requireClaims(ctx)
	if err != nil {
		return PendingToken{}, err
	}
	var row pendingTokenRow
	err = r.db.NewSelect().Model(&row).Where("machine_id = ? AND tenant_id = ? AND entity_id = ? AND owner_id = ?", machineID, claims.TenantID, claims.EntityID, claims.PrincipalID).Scan(ctx)
	if errors.Is(err, sql.ErrNoRows) {
		return PendingToken{}, ErrMachineNotFound
	}
	if err != nil {
		return PendingToken{}, fmt.Errorf("find pending machine token: %w", err)
	}
	return PendingToken{MachineID: row.MachineID, TenantID: row.TenantID, EntityID: row.EntityID, OwnerID: row.OwnerID, TokenHash: row.TokenHash, ExpiresAt: row.ExpiresAt, CreatedAt: row.CreatedAt}, nil
}

func (r *BunRepository) DeletePendingToken(ctx context.Context, machineID coreid.ID, tokenHash string) error {
	claims, err := requireClaims(ctx)
	if err != nil {
		return err
	}
	query := r.db.NewDelete().Model((*pendingTokenRow)(nil)).Where("machine_id = ? AND tenant_id = ? AND entity_id = ? AND owner_id = ?", machineID, claims.TenantID, claims.EntityID, claims.PrincipalID)
	if tokenHash != "" {
		query = query.Where("token_hash = ?", tokenHash)
	}
	if _, err := query.Exec(ctx); err != nil {
		return fmt.Errorf("delete pending machine token: %w", err)
	}
	return nil
}

func (r *BunRepository) AcquireRoute(ctx context.Context, machineID, holderID coreid.ID, ttl time.Duration) (RouteLease, error) {
	claims, err := requireClaims(ctx)
	if err != nil {
		return RouteLease{}, err
	}
	if machineID.Zero() || holderID.Zero() || ttl <= 0 {
		return RouteLease{}, fmt.Errorf("acquire machine route: machine, holder, and positive ttl are required")
	}
	var acquired RouteLease
	err = r.db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		now, err := r.databaseNow(ctx, tx)
		if err != nil {
			return err
		}
		var current connectionLeaseRow
		err = tx.NewSelect().Model(&current).Where("machine_id = ?", machineID).Scan(ctx)
		switch {
		case errors.Is(err, sql.ErrNoRows):
			row := connectionLeaseRow{MachineID: machineID, TenantID: claims.TenantID, EntityID: claims.EntityID, HolderID: holderID, FencingToken: 1, ExpiresAt: now + ttl.Milliseconds(), RuntimesJSON: "[]", UpdatedAt: now}
			if _, err := tx.NewInsert().Model(&row).Exec(ctx); err != nil {
				return ErrRouteHeld
			}
			acquired = routeFromRow(row)
			return nil
		case err != nil:
			return fmt.Errorf("load machine route: %w", err)
		case current.TenantID != claims.TenantID || current.EntityID != claims.EntityID:
			return ErrMachineNotFound
		case current.ExpiresAt > now:
			return ErrRouteHeld
		}
		row := current
		row.HolderID, row.FencingToken, row.ExpiresAt, row.UpdatedAt = holderID, current.FencingToken+1, now+ttl.Milliseconds(), now
		result, err := tx.NewUpdate().Model((*connectionLeaseRow)(nil)).Set("holder_id = ?", row.HolderID).Set("fencing_token = ?", row.FencingToken).
			Set("expires_at = ?", row.ExpiresAt).Set("updated_at = ?", now).
			Where("machine_id = ? AND fencing_token = ? AND expires_at <= ?", machineID, current.FencingToken, now).Exec(ctx)
		if err != nil {
			return fmt.Errorf("replace machine route: %w", err)
		}
		count, err := result.RowsAffected()
		if err != nil || count != 1 {
			return ErrRouteHeld
		}
		acquired = routeFromRow(row)
		return nil
	})
	if err != nil {
		return RouteLease{}, fmt.Errorf("acquire machine route %s: %w", machineID, err)
	}
	return acquired, nil
}

func (r *BunRepository) RenewRoute(ctx context.Context, route RouteLease, ttl time.Duration) (RouteLease, error) {
	now, err := r.databaseNow(ctx, r.db)
	if err != nil {
		return RouteLease{}, err
	}
	expiresAt := now + ttl.Milliseconds()
	result, err := r.db.NewUpdate().Model((*connectionLeaseRow)(nil)).Set("expires_at = ?", expiresAt).Set("updated_at = ?", now).
		Where("machine_id = ? AND tenant_id = ? AND entity_id = ? AND holder_id = ? AND fencing_token = ? AND expires_at > ?", route.MachineID, route.TenantID, route.EntityID, route.HolderID, route.FencingToken, now).Exec(ctx)
	if err != nil {
		return RouteLease{}, fmt.Errorf("renew machine route: %w", err)
	}
	count, err := result.RowsAffected()
	if err != nil || count != 1 {
		return RouteLease{}, ErrRouteStale
	}
	route.ExpiresAt = expiresAt
	return route, nil
}

func (r *BunRepository) ReleaseRoute(ctx context.Context, route RouteLease) error {
	now, err := r.databaseNow(ctx, r.db)
	if err != nil {
		return err
	}
	_, err = r.db.NewUpdate().Model((*connectionLeaseRow)(nil)).Set("expires_at = ?", now).Set("updated_at = ?", now).
		Where("machine_id = ? AND holder_id = ? AND fencing_token = ?", route.MachineID, route.HolderID, route.FencingToken).Exec(ctx)
	if err != nil {
		return fmt.Errorf("release machine route: %w", err)
	}
	return nil
}

func (r *BunRepository) RevokeRoute(ctx context.Context, machineID coreid.ID) error {
	claims, err := requireClaims(ctx)
	if err != nil {
		return err
	}
	now, err := r.databaseNow(ctx, r.db)
	if err != nil {
		return err
	}
	if _, err := r.db.NewUpdate().Model((*connectionLeaseRow)(nil)).Set("expires_at = ?", now).Set("updated_at = ?", now).
		Where("machine_id = ? AND tenant_id = ? AND entity_id = ?", machineID, claims.TenantID, claims.EntityID).Exec(ctx); err != nil {
		return fmt.Errorf("revoke machine route: %w", err)
	}
	return nil
}

func (r *BunRepository) UpdateRouteInventory(ctx context.Context, route RouteLease, name string, runtimes []string, osName, address string) error {
	payload, err := json.Marshal(runtimes)
	if err != nil {
		return fmt.Errorf("encode machine runtimes: %w", err)
	}
	result, err := r.db.NewUpdate().Model((*connectionLeaseRow)(nil)).Set("name = ?", name).Set("runtimes_json = ?", string(payload)).Set("os = ?", osName).Set("address = ?", address).
		Where("machine_id = ? AND holder_id = ? AND fencing_token = ?", route.MachineID, route.HolderID, route.FencingToken).Exec(ctx)
	if err != nil {
		return fmt.Errorf("update machine route inventory: %w", err)
	}
	count, err := result.RowsAffected()
	if err != nil || count != 1 {
		return ErrRouteStale
	}
	return nil
}

func (r *BunRepository) RouteInventory(ctx context.Context, machineID coreid.ID) (string, []string, string, string, error) {
	claims, err := requireClaims(ctx)
	if err != nil {
		return "", nil, "", "", err
	}
	var row connectionLeaseRow
	err = r.db.NewSelect().Model(&row).Where("machine_id = ? AND tenant_id = ? AND entity_id = ?", machineID, claims.TenantID, claims.EntityID).Scan(ctx)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil, "", "", ErrMachineNotFound
	}
	if err != nil {
		return "", nil, "", "", fmt.Errorf("load machine route inventory: %w", err)
	}
	var runtimes []string
	if err := json.Unmarshal([]byte(row.RuntimesJSON), &runtimes); err != nil {
		return "", nil, "", "", fmt.Errorf("decode machine runtimes: %w", err)
	}
	return row.Name, runtimes, row.OS, row.Address, nil
}

func (r *BunRepository) ResolveActiveRoute(ctx context.Context, machineID coreid.ID) (RouteLease, error) {
	claims, err := requireClaims(ctx)
	if err != nil {
		return RouteLease{}, err
	}
	now, err := r.databaseNow(ctx, r.db)
	if err != nil {
		return RouteLease{}, err
	}
	var row connectionLeaseRow
	err = r.db.NewSelect().Model(&row).Where("machine_id = ? AND tenant_id = ? AND entity_id = ? AND expires_at > ?", machineID, claims.TenantID, claims.EntityID, now).Scan(ctx)
	if errors.Is(err, sql.ErrNoRows) {
		return RouteLease{}, ErrMachineNotConnected
	}
	if err != nil {
		return RouteLease{}, fmt.Errorf("resolve active machine route: %w", err)
	}
	return routeFromRow(row), nil
}

func (r *BunRepository) ListActiveRoutes(ctx context.Context) ([]RouteLease, error) {
	claims, err := requireClaims(ctx)
	if err != nil {
		return nil, err
	}
	now, err := r.databaseNow(ctx, r.db)
	if err != nil {
		return nil, err
	}
	rows := []connectionLeaseRow{}
	if err := r.db.NewSelect().Model(&rows).Where("tenant_id = ? AND entity_id = ? AND expires_at > ?", claims.TenantID, claims.EntityID, now).Order("machine_id ASC").Scan(ctx); err != nil {
		return nil, fmt.Errorf("list active machine routes: %w", err)
	}
	routes := make([]RouteLease, 0, len(rows))
	for _, row := range rows {
		routes = append(routes, routeFromRow(row))
	}
	return routes, nil
}

func (r *BunRepository) RouteIsCurrent(ctx context.Context, route RouteLease) (bool, error) {
	now, err := r.databaseNow(ctx, r.db)
	if err != nil {
		return false, err
	}
	count, err := r.db.NewSelect().Model((*connectionLeaseRow)(nil)).Where("machine_id = ? AND tenant_id = ? AND entity_id = ? AND holder_id = ? AND fencing_token = ? AND expires_at > ?", route.MachineID, route.TenantID, route.EntityID, route.HolderID, route.FencingToken, now).Count(ctx)
	return count == 1, err
}

func routeFromRow(row connectionLeaseRow) RouteLease {
	return RouteLease{MachineID: row.MachineID, TenantID: row.TenantID, EntityID: row.EntityID, HolderID: row.HolderID, FencingToken: row.FencingToken, ExpiresAt: row.ExpiresAt}
}

func (r *BunRepository) Upsert(ctx context.Context, machine Machine) error {
	claims, err := requireClaims(ctx)
	if err != nil {
		return err
	}
	if machine.ID.Zero() {
		return fmt.Errorf("upsert machine: id is required")
	}
	if machine.TenantID.Zero() {
		machine.TenantID = claims.TenantID
	}
	if machine.EntityID.Zero() {
		machine.EntityID = claims.EntityID
	}
	if machine.OwnerID.Zero() {
		machine.OwnerID = claims.PrincipalID
	}
	if machine.TenantID != claims.TenantID || machine.EntityID != claims.EntityID || machine.OwnerID != claims.PrincipalID {
		return fmt.Errorf("upsert machine: ownership scope does not match authenticated claims")
	}
	now := time.Now().UTC().UnixMilli()
	actor := claims.PrincipalID
	if machine.RegisteredAt == 0 {
		machine.RegisteredAt = now
	}
	if machine.CreatedAt == 0 {
		machine.CreatedAt = now
	}
	if machine.CreatedBy.Zero() {
		machine.CreatedBy = actor
	}
	machine.UpdatedAt, machine.UpdatedBy = now, actor
	if machine.Version < 1 {
		machine.Version = 1
	}
	return r.db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		result, err := tx.NewUpdate().Model((*Machine)(nil)).
			Set("address = ?", machine.Address).
			Set("status = ?", machine.Status).
			Set("last_heartbeat_at = ?", machine.LastHeartbeatAt).
			Set("token_hash = ?", machine.TokenHash).
			Set("os = ?", machine.OS).
			Set("updated_at = ?", machine.UpdatedAt).
			Set("updated_by = ?", machine.UpdatedBy).
			Set("version = version + 1").
			Set("registered_at = CASE WHEN registered_at = 0 THEN ? ELSE registered_at END", machine.RegisteredAt).
			Where("id = ? AND tenant_id = ? AND entity_id = ? AND deleted_at = 0", machine.ID, claims.TenantID, claims.EntityID).
			Exec(ctx)
		if err != nil {
			return fmt.Errorf("update machine %s: %w", machine.ID, err)
		}
		updated, err := result.RowsAffected()
		if err != nil {
			return fmt.Errorf("inspect machine update %s: %w", machine.ID, err)
		}
		if updated > 0 {
			return nil
		}
		if _, err := tx.NewInsert().Model(&machine).Exec(ctx); err != nil {
			return fmt.Errorf("insert machine %s: %w", machine.ID, err)
		}
		return nil
	})
}

func (r *BunRepository) UpdateEntity(ctx context.Context, id, entityID coreid.ID) error {
	_, err := r.auditUpdate(ctx, id).Set("entity_id = ?", entityID).Exec(ctx)
	return wrapRepositoryError("update machine entity", err)
}

func (r *BunRepository) List(ctx context.Context) ([]Machine, error) {
	claims, err := requireClaims(ctx)
	if err != nil {
		return nil, err
	}
	items := make([]Machine, 0)
	query := r.db.NewSelect().Model(&items).
		Where("deleted_at = 0").
		Where("tenant_id = ?", claims.TenantID).
		Where("entity_id = ?", claims.EntityID)
	if err := query.Order("id ASC").Scan(ctx); err != nil {
		return nil, fmt.Errorf("list machines: %w", err)
	}
	return items, nil
}

// ListAll is reserved for trusted application lifecycle work such as token
// rehydration. Request handlers must use List so tenant/entity scope is mandatory.
func (r *BunRepository) ListAll(ctx context.Context) ([]Machine, error) {
	items := make([]Machine, 0)
	if err := r.db.NewSelect().Model(&items).Where("deleted_at = 0").Order("id ASC").Scan(ctx); err != nil {
		return nil, fmt.Errorf("list all machines: %w", err)
	}
	return items, nil
}

func (r *BunRepository) UpdateAddress(ctx context.Context, id coreid.ID, address string) error {
	_, err := r.auditUpdate(ctx, id).Set("address = ?", address).Exec(ctx)
	return wrapRepositoryError("update machine address", err)
}

func (r *BunRepository) TouchHeartbeat(ctx context.Context, id coreid.ID) error {
	_, err := r.auditUpdate(ctx, id).Set("last_heartbeat_at = ?", time.Now().UTC().UnixMilli()).Exec(ctx)
	return wrapRepositoryError("touch machine heartbeat", err)
}

func (r *BunRepository) UpdateToken(ctx context.Context, id coreid.ID, tokenHash string) error {
	_, err := r.auditUpdate(ctx, id).Set("token_hash = ?", tokenHash).Exec(ctx)
	return wrapRepositoryError("update machine token", err)
}

func (r *BunRepository) Delete(ctx context.Context, id coreid.ID) error {
	now := time.Now().UTC().UnixMilli()
	_, err := r.auditUpdate(ctx, id).
		Set("deleted_at = ?", now).
		Set("deleted_by = ?", authn.PrincipalIDFromContext(ctx)).
		Exec(ctx)
	return wrapRepositoryError("delete machine", err)
}

func (r *BunRepository) auditUpdate(ctx context.Context, id coreid.ID) *bun.UpdateQuery {
	claims, _ := authn.FromContext(ctx)
	if claims == nil {
		claims = &authn.Claims{}
	}
	return r.db.NewUpdate().Model((*Machine)(nil)).
		Set("updated_at = ?", time.Now().UTC().UnixMilli()).
		Set("updated_by = ?", authn.PrincipalIDFromContext(ctx)).
		Set("version = version + 1").
		Where("id = ? AND tenant_id = ? AND entity_id = ? AND deleted_at = 0", id, claims.TenantID, claims.EntityID)
}

func requireClaims(ctx context.Context) (*authn.Claims, error) {
	claims, ok := authn.FromContext(ctx)
	if !ok || claims.TenantID.Zero() || claims.EntityID.Zero() || claims.PrincipalID.Zero() {
		return nil, fmt.Errorf("machine repository requires authenticated tenant, entity, and principal claims")
	}
	return claims, nil
}

func wrapRepositoryError(operation string, err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("%s: %w", operation, err)
}
