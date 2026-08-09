-- 00002_card_agent.sql
-- +goose Up
ALTER TABLE cards ADD COLUMN agent TEXT
    CHECK (agent IN ('claude', 'opencode'));

-- +goose Down
ALTER TABLE cards DROP COLUMN agent;
