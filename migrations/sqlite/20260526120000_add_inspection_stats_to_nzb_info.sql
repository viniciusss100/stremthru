-- +goose Up
-- +goose StatementBegin
ALTER TABLE `nzb_info` ADD COLUMN `inspection_meta` jsonb;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE `nzb_info` DROP COLUMN `inspection_meta`;
-- +goose StatementEnd
