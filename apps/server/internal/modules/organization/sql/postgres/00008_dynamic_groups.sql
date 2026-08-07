-- +goose Up
ALTER TABLE iam_groups
    DROP CONSTRAINT iam_groups_group_type_check,
    ADD COLUMN rule_json VARCHAR(4096) NOT NULL DEFAULT '',
    ADD CONSTRAINT iam_groups_group_type_check CHECK (group_type IN ('static', 'dynamic')),
    ADD CONSTRAINT iam_groups_rule_check CHECK ((group_type = 'static' AND rule_json = '') OR (group_type = 'dynamic' AND rule_json <> ''));

-- +goose Down
ALTER TABLE iam_groups
    DROP CONSTRAINT iam_groups_rule_check,
    DROP CONSTRAINT iam_groups_group_type_check,
    DROP COLUMN rule_json,
    ADD CONSTRAINT iam_groups_group_type_check CHECK (group_type = 'static');
