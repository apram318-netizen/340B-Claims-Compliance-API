-- +goose Up 
CREATE TABLE upload_rows (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(), 
    batch_id UUID NOT NULL REFERENCES upload_batches(id), 
    row_number INTEGER NOT NULL, 
    data JSONB NOT NULL, 
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
); 

CREATE INDEX idx_upload_rows_batch_id ON upload_rows(batch_id); 

-- +goose Down
DROP INDEX IF EXISTS idx_upload_rows_batch_id; 
DROP TABlE IF EXISTS upload_rows; 