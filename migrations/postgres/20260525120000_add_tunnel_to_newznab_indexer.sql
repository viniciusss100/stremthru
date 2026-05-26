-- +goose Up
-- +goose StatementBegin
ALTER TABLE "public"."newznab_indexer"
  ADD COLUMN "tunnel" text;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE "public"."newznab_indexer"
  DROP COLUMN IF EXISTS "tunnel";
-- +goose StatementEnd
