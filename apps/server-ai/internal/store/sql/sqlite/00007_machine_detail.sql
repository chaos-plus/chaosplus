-- +goose Up
-- PRD D.3 machine 详情「关键信息」Tab:操作系统与注册时间。
ALTER TABLE machine_runners ADD COLUMN os TEXT NOT NULL DEFAULT '';
ALTER TABLE machine_runners ADD COLUMN registered_at BIGINT NOT NULL DEFAULT 0;

-- +goose Down
ALTER TABLE machine_runners DROP COLUMN registered_at;
ALTER TABLE machine_runners DROP COLUMN os;
