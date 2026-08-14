ALTER TABLE agent_runs ADD COLUMN request_id VARCHAR(64) NULL;

CREATE INDEX idx_agent_runs_created ON agent_runs (created_at);
