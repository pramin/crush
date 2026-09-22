-- +goose Up
-- +goose StatementBegin
ALTER TABLE sessions ADD COLUMN reported_at INTEGER;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE sessions DROP COLUMN reported_at;
-- +goose StatementEnd

