-- +goose Up
CREATE TABLE iam_tenants (
    id VARCHAR(128) PRIMARY KEY,
    slug VARCHAR(63) NOT NULL UNIQUE,
    name VARCHAR(128) NOT NULL,
    status VARCHAR(16) NOT NULL,
    version BIGINT NOT NULL,
    created_at BIGINT NOT NULL,
    updated_at BIGINT NOT NULL
) ENGINE=InnoDB;
-- +goose Down
DROP TABLE iam_tenants;
