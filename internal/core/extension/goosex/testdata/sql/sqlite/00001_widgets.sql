-- +goose Up
CREATE TABLE widgets (
    id   INTEGER NOT NULL PRIMARY KEY,
    name TEXT    NOT NULL
);

-- +goose Down
DROP TABLE widgets;
