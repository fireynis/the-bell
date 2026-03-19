#!/bin/bash
set -e

# Create the second database for Kratos.
# The primary database (bell) is created automatically via POSTGRES_DB.
psql -v ON_ERROR_STOP=1 --username "$POSTGRES_USER" --dbname "$POSTGRES_DB" <<-EOSQL
    CREATE DATABASE bell_kratos;
EOSQL
