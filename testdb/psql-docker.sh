#!/bin/bash

# Set default port if not specified
if [ -z "$POSTGRES_PORT" ]; then
    POSTGRES_PORT=9090
fi

# Prompt user for database name with default
read -p "Enter database name (default: tester1): " db_name

# Use default if no input provided
if [ -z "$db_name" ]; then
    db_name="tester1"
fi

echo "Connecting to PostgreSQL on port ${POSTGRES_PORT}..."
psql -h 127.0.0.1 -p ${POSTGRES_PORT} -U tester -d "$db_name"
