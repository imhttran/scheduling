#!/usr/bin/env bash
# Re-seed database (option 11 from manage.sh)
# Drops schema, then restarts backend to re-migrate and re-seed

set -e

echo ""
echo "==== Re-seed Database (Docker) ===="
echo ""
echo "This will:"
echo "  1. Drop all tables"
echo "  2. Restart backend (triggers migration + seeding)"
echo ""

read -r -p "Type 'yes' to confirm: " confirm
if [ "$confirm" != "yes" ]; then
  echo "Aborted."
  exit 0
fi

echo ""
echo "🔄 Dropping schema..."
docker-compose exec -T postgres psql -U postgres -d scheduling -c 'DROP SCHEMA public CASCADE; CREATE SCHEMA public;' || {
  echo "❌ Failed to drop schema"
  exit 1
}

echo "✓ Schema reset"
echo ""

echo "🔄 Restarting backend (re-migrating and re-seeding)..."
docker-compose restart backend

echo ""
sleep 3

echo "✓ Re-seeding complete"
echo ""
echo "Dev admin: admin@mail.edu"
docker-compose logs backend | grep -E "(applied migration|dev admin ready)" | tail -5
