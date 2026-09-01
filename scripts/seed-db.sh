#!/usr/bin/env bash
# Seed the database in Docker: resets schema and restarts backend to re-migrate and reseed

set -e

DATABASE_URL="${DATABASE_URL:-postgres://postgres:postgres@localhost:5432/scheduling?sslmode=disable}"

echo "🔄 Seeding database..."
echo "   Database: $DATABASE_URL"

# Check if psql is available (if running outside Docker)
if command -v psql &> /dev/null; then
    echo "📦 Dropping schema..."
    psql "$DATABASE_URL" -c 'DROP SCHEMA public CASCADE; CREATE SCHEMA public;' || true
    echo "✓ Schema reset"
    echo ""
    echo "The backend will re-migrate and re-seed from seed.csv on next start."
    echo "Restart backend: docker-compose restart backend"
else
    # For Docker, provide the command to run inside the container
    echo "📦 To seed database in Docker, run:"
    echo ""
    echo "  docker exec scheduling-db psql -U postgres -d scheduling -c 'DROP SCHEMA public CASCADE; CREATE SCHEMA public;'"
    echo "  docker-compose restart scheduling-backend"
    echo ""
fi
