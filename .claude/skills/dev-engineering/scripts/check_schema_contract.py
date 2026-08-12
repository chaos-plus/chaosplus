#!/usr/bin/env python3
"""检查高风险 Schema 契约违规。"""

from __future__ import annotations

import re
import sys
from pathlib import Path


ROOT = Path(__file__).resolve().parents[4]
TARGETS = (ROOT / "apps/server-ai/internal/modules", ROOT / "apps/server/internal/modules")
ID_COLUMN = re.compile(r"^\s*([a-z][a-z0-9_]*_id|id)\s+([A-Z]+(?:\([0-9, ]+\))?)\b", re.I)
TIME_COLUMN = re.compile(r"^\s*([a-z][a-z0-9_]*(?:_at|_time))\s+([A-Z]+(?:\([0-9, ]+\))?)\b", re.I)
CREATE_TABLE = re.compile(r"CREATE TABLE\s+(?:IF NOT EXISTS\s+)?(\w+)", re.I)
ALTER_TABLE = re.compile(r"ALTER TABLE\s+(\w+)", re.I)
# (table, column) pairs that are intentionally natural strings, credentials,
# hashes, or external identifiers and therefore must not be BIGINT.
EXEMPT_STRING_COLUMNS: dict[str, set[str]] = {
    "iam_tenants": {"slug"},
    "iam_principals": {"login_name", "email"},
    "iam_oauth_clients": {"name"},
    "iam_identity_providers": {"name", "issuer", "client_id", "client_secret_ciphertext", "scopes"},
    "iam_identity_links": {"external_subject", "email", "display_name"},
    "iam_saml_service_providers": {"name", "entity_id", "metadata_xml"},
    "iam_saml_idp_keys": {"key_id", "cert_pem", "key_ciphertext"},
    "iam_scim_directories": {"name", "name_key"},
    "iam_scim_credentials": {"name", "token_hash"},
    "iam_scim_resources": {"external_id", "external_key"},
    "iam_scim_targets": {"name", "name_key", "base_url", "bearer_token_ciphertext"},
    "iam_scim_target_resources": {"external_id"},
    "iam_invitations": {"email", "email_key", "token_hmac"},
    "iam_role_permissions": {"permission_code", "condition_json"},
    "iam_platform_grants": {"permission_code"},
    "iam_notification_outbox": {"kind", "recipient", "payload_ciphertext", "status", "last_error"},
    "iam_password_recovery_tokens": {"token_hmac"},
    "iam_email_verification_tokens": {"token_hmac", "email", "code"},
    "iam_sessions": {"id_hash", "ip_address", "user_agent"},
    "iam_refresh_tokens": {"id_hash", "family_id", "scope"},
    "iam_oauth_codes": {"code_hash", "redirect_uri", "scope", "code_challenge", "nonce"},
    "iam_oauth_consents": {"scope"},
    "iam_passkeys": {"id_hash", "name", "credential_ciphertext"},
    "iam_passkey_challenges": {"id_hash", "kind", "return_url", "session_data"},
    "iam_passkey_users": {"user_handle"},
    "iam_mfa_challenges": {"id_hash", "return_url"},
    "iam_stepup_challenges": {"id_hash", "session_hash"},
    "iam_credentials": {"password_hash", "totp_secret"},
    "iam_recovery_codes": {"code_hash"},
    "iam_audit_events": {"event_type", "target_type", "outcome", "ip_address", "user_agent", "detail"},
    "iam_audit_heads": {"event_hash"},
    "iam_service_accounts": {"description", "status"},
    "iam_service_account_credentials": {"name", "secret_hash", "scopes"},
    "iam_menus": {"label", "route", "icon", "permission_code", "status"},
    "iam_entities": {"type", "name", "status", "metadata"},
    "iam_role_bindings": {"scope_type", "effect"},
    "iam_relationships": {"subject_type", "subject_relation", "relation", "resource_type", "condition_json"},
    "iam_resource_relationships": {"subject_type", "subject_relation", "relation", "resource_type", "condition_json"},
    "iam_temporary_role_grants": {"source_type"},
    "iam_access_requests": {"role_name", "reason", "snapshot_json", "status", "decision_note", "revoke_reason"},
    "iam_approval_steps": {"step", "decision", "note"},
    "iam_access_reviews": {"name", "status"},
    "iam_access_review_items": {"principal_name", "role_name", "grant_type", "decision", "decision_note"},
    "iam_groups": {"name", "name_key", "group_type", "rule_json", "description", "status"},
    "iam_departments": {"name", "name_key", "status"},
    "iam_positions": {"code", "name", "status"},
    "iam_role_data_scopes": {"scope_type"},
    "authz_outbox": {"relationship_key", "resource_type", "resource_relation", "subject_type", "subject_relation", "operation", "status", "locked_by", "last_error", "zed_token"},
}


def sql_files() -> list[Path]:
    files: list[Path] = []
    for target in TARGETS:
        if target.exists():
            files.extend(
                path
                for path in target.rglob("*.sql")
                if len(path.parts) >= 3 and path.parent.parent.name == "sql"
            )
    return sorted(files)


def main() -> int:
    errors: list[str] = []
    grouped: dict[Path, set[str]] = {}
    for path in sql_files():
        relative = path.relative_to(ROOT)
        parts = relative.parts
        sql_index = parts.index("sql")
        module_root = Path(*parts[:sql_index])
        grouped.setdefault(module_root, set()).add(parts[sql_index + 1])
        current_table = ""
        for line_number, line in enumerate(path.read_text(encoding="utf-8").splitlines(), 1):
            create = CREATE_TABLE.search(line)
            if create:
                current_table = create.group(1)
            alter = ALTER_TABLE.search(line)
            if alter:
                current_table = alter.group(1)
            match = ID_COLUMN.match(line)
            column = match.group(1).lower() if match else ""
            if match and column not in EXEMPT_STRING_COLUMNS.get(current_table, set()):
                sql_type = match.group(2).upper()
                is_sqlite = parts[sql_index + 1] == "sqlite"
                valid_type = sql_type.startswith("BIGINT") or (is_sqlite and sql_type.startswith("INTEGER"))
                if not valid_type:
                    errors.append(f"{relative}:{line_number}: {current_table}.{match.group(1)} must use BIGINT, got {sql_type}")
            match = TIME_COLUMN.match(line)
            if match and not match.group(2).upper().startswith("BIGINT"):
                errors.append(f"{relative}:{line_number}: {match.group(1)} must use BIGINT UTC milliseconds")
    for module, dialects in grouped.items():
        missing = {"sqlite", "mysql", "postgres"} - dialects
        if missing:
            errors.append(f"{module}: missing SQL dialects: {', '.join(sorted(missing))}")
    if errors:
        print("schema contract violations:", file=sys.stderr)
        for error in errors:
            print(f"- {error}", file=sys.stderr)
        return 1
    print(f"schema contract OK ({len(sql_files())} migration files)")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
