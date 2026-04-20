-- +goose Up 
CREATE TABLE validation_results (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(), 
    row_id UUID NOT NULL REFERENCES upload_rows(id), 
    batch_id UUID NOT NULL REFERENCES upload_batches(id), 
    is_valid BOOLEAN NOT NULL, 
    errors JSONB, 
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_validation_results_batch_id ON validation_results(batch_id);

-- +goose Down
DROP INDEX IF EXISTS idx_validation_results_batch_id;
DROP TABLE IF EXISTS validation_results;
