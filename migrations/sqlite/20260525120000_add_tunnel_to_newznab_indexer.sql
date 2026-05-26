-- +goose Up
-- +goose StatementBegin
ALTER TABLE `newznab_indexer`
  ADD COLUMN `tunnel` text;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE `newznab_indexer`
  DROP COLUMN `tunnel`;
-- +goose StatementEnd
