-- name: CreateManufacturer :one
INSERT INTO manufacturers (name, labeler_code)
VALUES ($1, $2)
RETURNING *;

-- name: GetManufacturerByID :one
SELECT * FROM manufacturers WHERE id = $1;

-- name: GetManufacturerByLabelerCode :one
SELECT * FROM manufacturers WHERE labeler_code = $1;

-- name: ListManufacturers :many
SELECT * FROM manufacturers ORDER BY name;

-- name: ListManufacturersPaginated :many
SELECT * FROM manufacturers
ORDER BY name
LIMIT $1 OFFSET $2;

-- name: CountManufacturers :one
SELECT COUNT(*) FROM manufacturers;

-- name: CreateManufacturerProduct :one
INSERT INTO manufacturer_products (manufacturer_id, ndc, product_name)
VALUES ($1, $2, $3)
RETURNING *;

-- name: GetManufacturerByNDC :one
SELECT m.* FROM manufacturers m
JOIN manufacturer_products mp ON mp.manufacturer_id = m.id
WHERE mp.ndc = $1;

-- name: GetProductByNDC :one
SELECT * FROM manufacturer_products WHERE ndc = $1;

-- name: ListProductsByManufacturer :many
SELECT * FROM manufacturer_products
WHERE manufacturer_id = $1
ORDER BY ndc;

-- name: ListProductsByManufacturerPaginated :many
SELECT * FROM manufacturer_products
WHERE manufacturer_id = $1
ORDER BY ndc
LIMIT $2 OFFSET $3;

-- name: CountProductsByManufacturer :one
SELECT COUNT(*) FROM manufacturer_products
WHERE manufacturer_id = $1;
