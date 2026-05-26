-- +goose Up
-- +goose StatementBegin
CREATE TABLE `newznab_indexer_hostname` (
  `hostname` text NOT NULL PRIMARY KEY,
  `indexer_id` integer NOT NULL,
  `cat` datetime NOT NULL DEFAULT (unixepoch()),
  `uat` datetime NOT NULL DEFAULT (unixepoch())
);
CREATE INDEX `newznab_indexer_hostname_idx_indexer_id` ON `newznab_indexer_hostname` (`indexer_id`);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE `newznab_indexer_hostname`;
-- +goose StatementEnd