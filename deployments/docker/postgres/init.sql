-- Service-owned schemas and least-privilege roles (Phase 1).
-- Application migrations add domain tables within these schemas.

CREATE SCHEMA IF NOT EXISTS sale;
CREATE SCHEMA IF NOT EXISTS admission;
CREATE SCHEMA IF NOT EXISTS inventory;
CREATE SCHEMA IF NOT EXISTS "order";
CREATE SCHEMA IF NOT EXISTS payment;

DO $$
BEGIN
  IF NOT EXISTS (SELECT FROM pg_roles WHERE rolname = 'sale_app') THEN
    CREATE ROLE sale_app LOGIN PASSWORD 'sale_app';
  END IF;
  IF NOT EXISTS (SELECT FROM pg_roles WHERE rolname = 'admission_app') THEN
    CREATE ROLE admission_app LOGIN PASSWORD 'admission_app';
  END IF;
  IF NOT EXISTS (SELECT FROM pg_roles WHERE rolname = 'inventory_app') THEN
    CREATE ROLE inventory_app LOGIN PASSWORD 'inventory_app';
  END IF;
  IF NOT EXISTS (SELECT FROM pg_roles WHERE rolname = 'order_app') THEN
    CREATE ROLE order_app LOGIN PASSWORD 'order_app';
  END IF;
  IF NOT EXISTS (SELECT FROM pg_roles WHERE rolname = 'payment_app') THEN
    CREATE ROLE payment_app LOGIN PASSWORD 'payment_app';
  END IF;
END
$$;

GRANT USAGE, CREATE ON SCHEMA sale TO sale_app;
GRANT USAGE, CREATE ON SCHEMA admission TO admission_app;
GRANT USAGE, CREATE ON SCHEMA inventory TO inventory_app;
GRANT USAGE, CREATE ON SCHEMA "order" TO order_app;
GRANT USAGE, CREATE ON SCHEMA payment TO payment_app;

ALTER ROLE sale_app SET search_path TO sale;
ALTER ROLE admission_app SET search_path TO admission;
ALTER ROLE inventory_app SET search_path TO inventory;
ALTER ROLE order_app SET search_path TO "order";
ALTER ROLE payment_app SET search_path TO payment;

-- Superuser gateflow retains admin for local migrations.
GRANT ALL ON SCHEMA sale, admission, inventory, "order", payment TO gateflow;
