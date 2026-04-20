-- name: CreateValidationResult :one
INSERT INTO validation_results (row_id,batch_id,is_valid,errors)
VALUES($1,$2,$3,$4)
RETURNING *;

