-- +goose Up
-- +goose StatementBegin
CREATE TABLE "public"."newznab_indexer_hostname" (
  "hostname" text NOT NULL PRIMARY KEY,
  "indexer_id" bigint NOT NULL,
  "cat" timestamptz NOT NULL DEFAULT CURRENT_TIMESTAMP,
  "uat" timestamptz NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX "newznab_indexer_hostname_idx_indexer_id" ON "public"."newznab_indexer_hostname" ("indexer_id");
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS "public"."newznab_indexer_hostname";
-- +goose StatementEnd