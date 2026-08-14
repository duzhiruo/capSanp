CREATE TABLE IF NOT EXISTS devices (
  id VARCHAR(64) PRIMARY KEY,
  created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

CREATE TABLE IF NOT EXISTS screenshots (
  id VARCHAR(64) PRIMARY KEY,
  device_id VARCHAR(64) NOT NULL,
  original_filename VARCHAR(255) NOT NULL,
  storage_path VARCHAR(1024) NOT NULL,
  status VARCHAR(32) NOT NULL,
  ocr_text LONGTEXT NULL,
  summary TEXT NULL,
  category VARCHAR(128) NULL,
  tags_text TEXT NULL,
  explanation TEXT NULL,
  created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  INDEX idx_screenshots_device_created (device_id, created_at),
  FULLTEXT KEY ft_screenshots_text (ocr_text, summary, tags_text),
  CONSTRAINT fk_screenshots_device FOREIGN KEY (device_id) REFERENCES devices(id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

CREATE TABLE IF NOT EXISTS agent_runs (
  id VARCHAR(64) PRIMARY KEY,
  device_id VARCHAR(64) NOT NULL,
  screenshot_id VARCHAR(64) NULL,
  type VARCHAR(64) NOT NULL,
  status VARCHAR(32) NOT NULL,
  input_json JSON NULL,
  output_json JSON NULL,
  error_message TEXT NULL,
  duration_ms BIGINT NOT NULL DEFAULT 0,
  created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  INDEX idx_agent_runs_device_created (device_id, created_at),
  INDEX idx_agent_runs_screenshot (screenshot_id),
  CONSTRAINT fk_agent_runs_device FOREIGN KEY (device_id) REFERENCES devices(id),
  CONSTRAINT fk_agent_runs_screenshot FOREIGN KEY (screenshot_id) REFERENCES screenshots(id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

CREATE TABLE IF NOT EXISTS agent_steps (
  id BIGINT AUTO_INCREMENT PRIMARY KEY,
  run_id VARCHAR(64) NOT NULL,
  name VARCHAR(128) NOT NULL,
  status VARCHAR(32) NOT NULL,
  input_json JSON NULL,
  output_json JSON NULL,
  error_message TEXT NULL,
  duration_ms BIGINT NOT NULL DEFAULT 0,
  created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
  INDEX idx_agent_steps_run (run_id, id),
  CONSTRAINT fk_agent_steps_run FOREIGN KEY (run_id) REFERENCES agent_runs(id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

CREATE TABLE IF NOT EXISTS tool_calls (
  id BIGINT AUTO_INCREMENT PRIMARY KEY,
  run_id VARCHAR(64) NOT NULL,
  step_id BIGINT NULL,
  tool_name VARCHAR(128) NOT NULL,
  input_json JSON NULL,
  output_json JSON NULL,
  error_message TEXT NULL,
  duration_ms BIGINT NOT NULL DEFAULT 0,
  created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
  INDEX idx_tool_calls_run (run_id, id),
  CONSTRAINT fk_tool_calls_run FOREIGN KEY (run_id) REFERENCES agent_runs(id),
  CONSTRAINT fk_tool_calls_step FOREIGN KEY (step_id) REFERENCES agent_steps(id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

CREATE TABLE IF NOT EXISTS llm_calls (
  id BIGINT AUTO_INCREMENT PRIMARY KEY,
  run_id VARCHAR(64) NOT NULL,
  provider VARCHAR(64) NOT NULL,
  model VARCHAR(128) NOT NULL,
  prompt LONGTEXT NOT NULL,
  response LONGTEXT NULL,
  prompt_tokens INT NOT NULL DEFAULT 0,
  completion_tokens INT NOT NULL DEFAULT 0,
  estimated_cost_usd DECIMAL(12, 8) NOT NULL DEFAULT 0,
  duration_ms BIGINT NOT NULL DEFAULT 0,
  created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
  INDEX idx_llm_calls_run (run_id, id),
  CONSTRAINT fk_llm_calls_run FOREIGN KEY (run_id) REFERENCES agent_runs(id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

CREATE TABLE IF NOT EXISTS memories (
  id BIGINT AUTO_INCREMENT PRIMARY KEY,
  device_id VARCHAR(64) NOT NULL,
  kind VARCHAR(64) NOT NULL,
  content_json JSON NOT NULL,
  created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
  INDEX idx_memories_device_kind (device_id, kind),
  CONSTRAINT fk_memories_device FOREIGN KEY (device_id) REFERENCES devices(id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;
