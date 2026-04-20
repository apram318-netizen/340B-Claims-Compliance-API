-- +goose Up

CREATE TABLE manufacturers (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name         TEXT NOT NULL,
    labeler_code TEXT NOT NULL UNIQUE, -- first 5 digits of NDC, e.g. "00069"
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE manufacturer_products (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    manufacturer_id UUID NOT NULL REFERENCES manufacturers(id),
    ndc             TEXT NOT NULL UNIQUE,
    product_name    TEXT,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_manufacturer_products_manufacturer ON manufacturer_products(manufacturer_id);
CREATE INDEX idx_manufacturer_products_ndc ON manufacturer_products(ndc);

-- +goose Down
DROP INDEX IF EXISTS idx_manufacturer_products_ndc;
DROP INDEX IF EXISTS idx_manufacturer_products_manufacturer;
DROP TABLE IF EXISTS manufacturer_products;
DROP TABLE IF EXISTS manufacturers;
