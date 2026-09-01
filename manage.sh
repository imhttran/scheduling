#!/usr/bin/env bash
# Single entry point for the template: start/stop/status, tests, setup,
# role management, database reset.
# Backend: Go + chi + PostgreSQL (backend/). Frontend: Next.js (frontend/).

set -u

ROOT_DIR="$(cd "$(dirname "$0")" && pwd)"
GREEN='\033[32m'; YELLOW='\033[33m'; RED='\033[31m'; NC='\033[0m'

PORT_BACKEND=8080
PORT_FRONTEND=3000

# Load env files the way the Go backend does (personal .env wins, .env.dev
# fills in for development) — only used by the reset-database command.
load_db_url() {
  local url="postgres://postgres:postgres@localhost:5432/go_template?sslmode=disable"
  if [ -f "$ROOT_DIR/.env" ]; then
    url=$(grep -E '^DATABASE_URL=' "$ROOT_DIR/.env" | tail -1 | cut -d= -f2- | tr -d '"' || true)
  fi
  if [ ! -f "$ROOT_DIR/.env" ] && [ -f "$ROOT_DIR/.env.dev" ]; then
    url=$(grep -E '^DATABASE_URL=' "$ROOT_DIR/.env.dev" | tail -1 | cut -d= -f2- | tr -d '"')
  fi
  echo "$url"
}

wait_for_port() {
  local port="$1" name="$2" i=0
  until lsof -nP -iTCP:"$port" -sTCP:LISTEN >/dev/null 2>&1; do
    i=$((i + 1))
    if [ "$i" -ge 20 ]; then
      echo -e "${RED}$name did not come up on :$port${NC}"
      return 1
    fi
    sleep 1
  done
}

# start_service <Name> <dir> <port> <cmd...> — checks the port is free,
# backgrounds <cmd> in <dir>, waits for the port, writes the PID to <dir>/<dir>.pid.
start_service() {
  local name="$1" dir="$2" port="$3"; shift 3
  if lsof -nP -iTCP:"$port" -sTCP:LISTEN >/dev/null 2>&1; then
    echo -e "${YELLOW}$name already running on :$port${NC}"
    return 0
  fi
  echo "Starting $name on :$port ..."
  (cd "$ROOT_DIR/$dir" && "$@" > "/tmp/go-template-$dir.log" 2>&1 &)
  wait_for_port "$port" "$name" || return 1
  # PID of the actual listening process (go run's child binary / next dev).
  local pid
  pid=$(lsof -nP -tiTCP:"$port" -sTCP:LISTEN | head -1)
  if [ -n "$pid" ]; then echo "$pid" > "$ROOT_DIR/$dir/$dir.pid"; fi
}

# start_backend — refuses to pointlessly launch if PostgreSQL is down (the
# backend exits immediately anyway). Reuses load_db_url.
start_backend() {
  local url
  url=$(load_db_url)
  if command -v pg_isready >/dev/null 2>&1 && ! pg_isready -q -d "$url"; then
    echo -e "${RED}PostgreSQL is not running (checked $url).${NC}"
    echo "Start it first, e.g. brew services start postgresql@16"
    return 1
  fi
  if ! start_service "Backend" backend "$PORT_BACKEND" go run .; then
    echo "→ see /tmp/go-template-backend.log"
    return 1
  fi
}

stop_service() {
  local dir="$1" name="$2" port="$3"
  # Separate statement: in one `local` line every RHS is expanded before any
  # assignment happens, so $dir above would still be unbound here (set -u).
  local pid_file="$ROOT_DIR/$dir/$dir.pid"
  local pid=""
  if [ -f "$pid_file" ]; then
    pid=$(cat "$pid_file")
    rm -f "$pid_file"
  fi
  # Fall back to whatever is listening on the port (e.g. started manually,
  # or a stale pid file) so Stop All always works.
  if [ -z "$pid" ]; then
    pid=$(lsof -nP -tiTCP:"$port" -sTCP:LISTEN 2>/dev/null | head -1)
  fi
  if [ -n "$pid" ] && kill "$pid" 2>/dev/null; then
    echo "Stopped $name (pid $pid)"
  else
    echo -e "${YELLOW}$name is not running${NC}"
  fi
}

start_all() {
  start_backend || return 1
  start_service "Frontend" frontend "$PORT_FRONTEND" npm run dev
  echo -e "${GREEN}Backend: http://localhost:$PORT_BACKEND  Frontend: http://localhost:$PORT_FRONTEND${NC}"
  echo "Logs: /tmp/go-template-backend.log, /tmp/go-template-frontend.log"
}

run_backend_tests() {
  if [ -n "${TEST_DATABASE_URL:-}" ]; then
    (cd "$ROOT_DIR/backend" && TEST_DATABASE_URL="$TEST_DATABASE_URL" go test ./...) || return 1
  else
    echo -e "${YELLOW}TEST_DATABASE_URL not set — unit-only tests (integration tests skip).${NC}"
    (cd "$ROOT_DIR/backend" && go test ./...) || return 1
  fi
}

first_time_setup() {
  echo "→ frontend: npm install"
  (cd "$ROOT_DIR/frontend" && npm install) || return 1
  echo "→ backend: go mod download"
  (cd "$ROOT_DIR/backend" && go mod download) || return 1
  echo "→ database: migrations apply automatically on backend start."
  echo "  Requires a running PostgreSQL (see DATABASE_URL in .env.example)."
  echo -e "${GREEN}Setup complete. Start everything with option 1.${NC}"
}

reset_database() {
  local url
  url=$(load_db_url)
  echo -e "${RED}This drops ALL tables in: ${url}${NC}"
  read -r -p "Type 'yes' to confirm: " confirm
  if [ "$confirm" != "yes" ]; then echo "Aborted."; return 0; fi
  psql "$url" -c 'DROP SCHEMA public CASCADE; CREATE SCHEMA public;' || return 1
  echo -e "${GREEN}Database reset. Tables re-apply on next backend start.${NC}"
}

# Drop the schema, then restart the backend so it re-migrates and re-seeds
# from backend/seed.csv. One-shot "start fresh with seed data".
re_seed() {
  local url
  url=$(load_db_url)
  echo -e "${RED}This drops ALL tables in: ${url}${NC}"
  read -r -p "Type 'yes' to confirm: " confirm
  if [ "$confirm" != "yes" ]; then echo "Aborted."; return 0; fi
  psql "$url" -c 'DROP SCHEMA public CASCADE; CREATE SCHEMA public;' || return 1
  stop_service backend Backend "$PORT_BACKEND"
  start_backend || return 1
  echo -e "${GREEN}Database re-seeded.${NC}"
}

set_user_role() {
  read -r -p "Email: " email
  read -r -p "Role (client/staff/admin): " role
  (cd "$ROOT_DIR/backend" && go run ./cmd/set-role "$email" "$role") || return 1
}

show_status() {
  if lsof -nP -iTCP:"$PORT_BACKEND" -sTCP:LISTEN >/dev/null 2>&1; then
    echo -e "  Backend : ${GREEN}running${NC} on :$PORT_BACKEND"
  else
    echo -e "  Backend : ${RED}not running${NC}"
  fi
  if lsof -nP -iTCP:"$PORT_FRONTEND" -sTCP:LISTEN >/dev/null 2>&1; then
    echo -e "  Frontend: ${GREEN}running${NC} on :$PORT_FRONTEND"
  else
    echo -e "  Frontend: ${RED}not running${NC}"
  fi
}

# Tail a service log. Ctrl-C to stop following.
view_logs() {
  echo "Which log?"
  echo "  b) Backend"
  echo "  f) Frontend"
  echo "  a) Both"
  read -r -p "Choose: " which
  case "$which" in
    b) tail -f /tmp/go-template-backend.log ;;
    f) tail -f /tmp/go-template-frontend.log ;;
    a) tail -f /tmp/go-template-backend.log /tmp/go-template-frontend.log ;;
    *) echo -e "${YELLOW}Unknown option${NC}" ;;
  esac
}

while true; do
  echo ""
  echo "==== Go + Next.js template ===="
  echo " 1) Start All (Backend + Frontend)"
  echo " 2) Start Backend only"
  echo " 3) Start Frontend only"
  echo " 4) Stop All"
  echo " 5) Status"
  echo " 6) Run Tests (backend go test + frontend build)"
  echo " 7) First-Time Setup (install deps)"
  echo " 8) Set User Role"
  echo " 9) Reset Database (destructive)"
  echo " 10) View Logs (tail)"
  echo " 11) Re-seed (reset DB + restart backend)"
  echo " q) Quit"
  read -r -p "Choose: " choice
  case "$choice" in
    1) start_all ;;
    2) start_backend ;;
    3) start_service "Frontend" frontend "$PORT_FRONTEND" npm run dev ;;
    4)
      stop_service backend Backend "$PORT_BACKEND"
      stop_service frontend Frontend "$PORT_FRONTEND"
      ;;
    5) show_status ;;
    6)
      run_backend_tests || { echo -e "${RED}Backend tests failed${NC}"; continue; }
      (cd "$ROOT_DIR/frontend" && npm run build) || echo -e "${RED}Frontend build failed${NC}"
      ;;
    7) first_time_setup ;;
    8) set_user_role ;;
    9) reset_database ;;
    10) view_logs ;;
    11) re_seed ;;
    q) break ;;
    *) echo -e "${YELLOW}Unknown option${NC}" ;;
  esac
done
