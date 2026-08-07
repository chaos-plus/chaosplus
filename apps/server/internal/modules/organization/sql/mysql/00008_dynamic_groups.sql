-- +goose Up
ALTER TABLE iam_groups
    DROP CHECK chk_iam_groups_type,
    ADD COLUMN rule_json VARCHAR(4096) NOT NULL DEFAULT '' AFTER group_type,
    ADD CONSTRAINT chk_iam_groups_type CHECK (group_type IN ('static', 'dynamic')),
    ADD CONSTRAINT chk_iam_groups_rule CHECK ((group_type = 'static' AND rule_json = '') OR (group_type = 'dynamic' AND rule_json <> ''));

-- +goose Down
ALTER TABLE iam_groups
    DROP CHECK chk_iam_groups_rule,
    DROP CHECK chk_iam_groups_type,
    DROP COLUMN rule_json,
    ADD CONSTRAINT chk_iam_groups_type CHECK (group_type = 'static');
