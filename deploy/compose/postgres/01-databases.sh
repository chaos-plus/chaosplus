#!/bin/sh
set -eu

psql -v ON_ERROR_STOP=1 \
  --username "$POSTGRES_USER" \
  --dbname "$POSTGRES_DB" \
  --set=zitadel_password="$ZITADEL_DB_PASSWORD" \
  --set=spicedb_password="$SPICEDB_DB_PASSWORD" \
  --set=migrator_password="$CHAOSPLUS_MIGRATOR_PASSWORD" \
  --set=runtime_password="$CHAOSPLUS_RUNTIME_PASSWORD" <<-'SQL'
SELECT format('CREATE ROLE zitadel LOGIN PASSWORD %L', :'zitadel_password')
WHERE NOT EXISTS (SELECT FROM pg_roles WHERE rolname = 'zitadel') \gexec
SELECT format('CREATE ROLE spicedb LOGIN PASSWORD %L', :'spicedb_password')
WHERE NOT EXISTS (SELECT FROM pg_roles WHERE rolname = 'spicedb') \gexec
SELECT format('CREATE ROLE chaosplus_migrator LOGIN PASSWORD %L', :'migrator_password')
WHERE NOT EXISTS (SELECT FROM pg_roles WHERE rolname = 'chaosplus_migrator') \gexec
SELECT format('CREATE ROLE chaosplus_app LOGIN PASSWORD %L', :'runtime_password')
WHERE NOT EXISTS (SELECT FROM pg_roles WHERE rolname = 'chaosplus_app') \gexec

SELECT 'CREATE DATABASE zitadel OWNER zitadel'
WHERE NOT EXISTS (SELECT FROM pg_database WHERE datname = 'zitadel') \gexec
SELECT 'CREATE DATABASE spicedb OWNER spicedb'
WHERE NOT EXISTS (SELECT FROM pg_database WHERE datname = 'spicedb') \gexec
SELECT 'CREATE DATABASE chaosplus OWNER chaosplus_migrator'
WHERE NOT EXISTS (SELECT FROM pg_database WHERE datname = 'chaosplus') \gexec
SQL

psql -v ON_ERROR_STOP=1 --username "$POSTGRES_USER" --dbname chaosplus <<-'SQL'
REVOKE CREATE ON SCHEMA public FROM PUBLIC;
GRANT CONNECT ON DATABASE chaosplus TO chaosplus_app;
GRANT USAGE ON SCHEMA public TO chaosplus_app;
ALTER DEFAULT PRIVILEGES FOR ROLE chaosplus_migrator IN SCHEMA public
  GRANT SELECT, INSERT, UPDATE, DELETE ON TABLES TO chaosplus_app;
ALTER DEFAULT PRIVILEGES FOR ROLE chaosplus_migrator IN SCHEMA public
  GRANT USAGE, SELECT ON SEQUENCES TO chaosplus_app;
SQL
