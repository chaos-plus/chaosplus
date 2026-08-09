package app

import (
	"context"
	"path/filepath"
	"testing"

	"database/sql"

	"github.com/uptrace/bun"
	"github.com/uptrace/bun/dialect/sqlitedialect"
	_ "github.com/uptrace/bun/driver/sqliteshim"
)

// openTestDB spins up a file-backed sqlite DB with the two tables the verified
// hook writes, so the hook can be exercised without the full module stack.
func openTestDB(t *testing.T) *bun.DB {
	t.Helper()
	sqldb, err := sql.Open("sqliteshim", filepath.Join(t.TempDir(), "iam.db"))
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { _ = sqldb.Close() })
	db := bun.NewDB(sqldb, sqlitedialect.New())
	if _, err := db.Exec(`CREATE TABLE iam_tenants (id TEXT PRIMARY KEY, slug TEXT UNIQUE, name TEXT, status TEXT, version INTEGER, created_at INTEGER, updated_at INTEGER)`); err != nil {
		t.Fatalf("create tenants: %v", err)
	}
	if _, err := db.Exec(`CREATE TABLE iam_tenant_members (tenant_id TEXT, user_subject TEXT, display_name TEXT, email TEXT, status TEXT, created_at INTEGER, updated_at INTEGER, PRIMARY KEY (tenant_id, user_subject))`); err != nil {
		t.Fatalf("create members: %v", err)
	}
	return db
}

func TestBootstrapTenantForVerifiedUser(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)

	if err := bootstrapTenantForVerifiedUser(ctx, db, "pr-1", "dev@chaos.plus"); err != nil {
		t.Fatalf("bootstrap: %v", err)
	}

	var tenants int
	if err := db.NewSelect().Table("iam_tenants").ColumnExpr("COUNT(*)").Scan(ctx, &tenants); err != nil {
		t.Fatalf("count tenants: %v", err)
	}
	if tenants != 1 {
		t.Fatalf("expected 1 tenant, got %d", tenants)
	}
	var slug string
	if err := db.NewSelect().Table("iam_tenants").Column("slug").Scan(ctx, &slug); err != nil {
		t.Fatalf("get slug: %v", err)
	}
	if len(slug) < 5 {
		t.Fatalf("slug too short: %q", slug)
	}

	var members int
	if err := db.NewSelect().Table("iam_tenant_members").ColumnExpr("COUNT(*)").Scan(ctx, &members); err != nil {
		t.Fatalf("count members: %v", err)
	}
	if members != 1 {
		t.Fatalf("expected 1 member, got %d", members)
	}
	var subject string
	if err := db.NewSelect().Table("iam_tenant_members").Column("user_subject").Scan(ctx, &subject); err != nil {
		t.Fatalf("get subject: %v", err)
	}
	if subject != "pr-1" {
		t.Fatalf("member subject = %q, want pr-1", subject)
	}
}

func TestBootstrapTenantRejectsNilDB(t *testing.T) {
	if err := bootstrapTenantForVerifiedUser(context.Background(), nil, "pr-1", "a@b.c"); err == nil {
		t.Fatal("nil DB must error")
	}
}

// owner 角色与 tenant_administer 权限也要一起建,否则用户只是普通成员,
// 无法管理自己的租户(/iam/tenants 会 403)。
func TestBootstrapTenantGrantsOwnerRole(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	if err := db.Exec(`CREATE TABLE iam_roles (tenant_id TEXT, id TEXT, name TEXT, description TEXT, created_at INTEGER, updated_at INTEGER, PRIMARY KEY (tenant_id,id))`); err != nil {
		t.Fatalf("create roles: %v", err)
	}
	if err := db.Exec(`CREATE TABLE iam_role_permissions (tenant_id TEXT, role_id TEXT, permission_code TEXT, condition_json TEXT, created_at INTEGER, PRIMARY KEY (tenant_id,role_id,permission_code))`); err != nil {
		t.Fatalf("create perms: %v", err)
	}
	if err := db.Exec(`CREATE TABLE iam_role_members (tenant_id TEXT, role_id TEXT, user_subject TEXT, created_at INTEGER, PRIMARY KEY (tenant_id,role_id,user_subject))`); err != nil {
		t.Fatalf("create role members: %v", err)
	}

	if err := bootstrapTenantForVerifiedUser(ctx, db, "pr-owner", "o@chaos.plus"); err != nil {
		t.Fatalf("bootstrap: %v", err)
	}
	var perms int
	if err := db.NewSelect().Table("iam_role_permissions").Where("permission_code = ?", "tenant_administer").ColumnExpr("COUNT(*)").Scan(ctx, &perms); err != nil {
		t.Fatalf("count perms: %v", err)
	}
	if perms != 1 {
		t.Fatalf("expected tenant_administer permission, got %d", perms)
	}
	var roleMembers int
	if err := db.NewSelect().Table("iam_role_members").Where("user_subject = ?", "pr-owner").ColumnExpr("COUNT(*)").Scan(ctx, &roleMembers); err != nil {
		t.Fatalf("count role members: %v", err)
	}
	if roleMembers != 1 {
		t.Fatalf("expected owner role member, got %d", roleMembers)
	}
}
