# Docker Setup for Scheduling App

This project uses Docker for containerization and is optimized for Rancher deployment.

## Quick Start

### Build and run locally with Docker Compose

**Easiest way (using Make):**
```bash
make docker-up
```

**Or with docker-compose directly:**
```bash
docker-compose up --build
```

This starts:
- **Backend:** http://localhost:8081
- **Frontend:** http://localhost:3001
- **PostgreSQL:** localhost:5433

(Ports differ from `manage.sh` to avoid conflicts)

### Configure Ports

By default, Docker uses different ports from `manage.sh` to avoid conflicts:
- Frontend: **3001** (vs 3000 in manage.sh)
- Backend: **8081** (vs 8080 in manage.sh)
- PostgreSQL: **5433** (vs 5432)

**To customize ports**, create a `.env` file:

```bash
cp .env.docker .env
# Edit .env and change:
FRONTEND_PORT=3000
BACKEND_PORT=8080
POSTGRES_PORT=5432
```

Then restart:
```bash
make docker-down
make docker-up
# or: docker-compose up --build
```

## Makefile Commands

A `Makefile` provides quick shortcuts for common Docker operations. Instead of typing long `docker-compose` commands, use `make`:

### Available Commands

```bash
make docker-up         # Start all services (backend, frontend, postgres)
make docker-down       # Stop all services
make docker-status     # Show service status
make docker-logs       # Follow logs from all services
make docker-build      # Build images without starting
make docker-rebuild    # Clean rebuild (down + up --build)
make docker-reseed     # Re-seed database (equivalent to manage.sh option 11)
make reseed            # Shortcut for docker-reseed
make help              # Show all available commands
```

### Examples

**Start fresh:**
```bash
make docker-up
# → Services running on:
#   Frontend: http://localhost:3001
#   Backend:  http://localhost:8081
#   Postgres: localhost:5433
```

**Check status:**
```bash
make docker-status
# → Shows NAME, IMAGE, STATUS, PORTS for all containers
```

**Follow logs:**
```bash
make docker-logs
# → Real-time logs from all services (Ctrl+C to stop)
```

**Re-seed database:**
```bash
make docker-reseed
# → Interactive: asks for confirmation, then:
#   1. Drops all tables
#   2. Restarts backend (triggers migration + seeding)
#   3. Shows migrations applied
```

**Clean rebuild:**
```bash
make docker-rebuild
# → Equivalent to:
#   docker-compose down
#   docker-compose up --build -d
#   Then shows running services
```

### Combining Commands

```bash
# Start, follow logs, then stop
make docker-up
make docker-logs       # Ctrl+C to stop following
make docker-down

# Rebuild and reseed
make docker-rebuild
make docker-reseed
```

### Comparison: Make vs Docker Compose vs manage.sh

| Task | Make | Docker Compose | manage.sh |
|------|------|----------------|-----------|
| Start services | `make docker-up` | `docker-compose up -d` | N/A (Docker only) |
| Stop services | `make docker-down` | `docker-compose down` | N/A (Docker only) |
| Re-seed database | `make docker-reseed` | `bash scripts/reseed.sh` | Option 11 |
| Show status | `make docker-status` | `docker-compose ps` | Option 5 |
| Follow logs | `make docker-logs` | `docker-compose logs -f` | Option 10 |

**Note:** `manage.sh` runs backend/frontend locally (not in Docker). Use that for local development without containers.

### Re-seed Database (Option 11 from manage.sh)

Quick command to reset and re-seed (like `manage.sh` option 11):

```bash
# Via Make
make docker-reseed

# Or directly
bash scripts/reseed.sh
```

This:
1. Drops all tables
2. Restarts backend (auto-migrates and re-seeds)
3. Shows confirmation

The database auto-migrates on backend startup. Dev admin (`admin@mail.edu`) is always created.

### Build individual images

**Backend:**
```bash
docker build -t scheduling-backend:latest ./backend
docker run -p 8081:8080 -e DATABASE_URL=postgres://user:pass@host/db scheduling-backend
```

**Frontend:**
```bash
docker build -t scheduling-frontend:latest ./frontend
docker run -p 3001:3000 scheduling-frontend
```

## Image Details

### Backend
- **Base:** Alpine Linux (minimal, ~10-50MB final image)
- **Runtime:** Go binary compiled with CGO disabled
- **Health Check:** Built-in (expects `-health` flag support)
- **User:** Non-root (UID 1000) for security

### Frontend
- **Base:** Node.js 20 Alpine
- **Build:** Next.js standalone output (most efficient)
- **Size:** ~70-100MB final image
- **Health Check:** HTTP GET to `/health`
- **User:** Non-root (UID 1000) for security

## Rancher Deployment

These images are production-ready for Rancher. To deploy:

1. **Build and push to registry:**
   ```bash
   docker build -t your-registry/scheduling-backend:latest ./backend
   docker push your-registry/scheduling-backend:latest
   
   docker build -t your-registry/scheduling-frontend:latest ./frontend
   docker push your-registry/scheduling-frontend:latest
   ```

2. **Deploy in Rancher:**
   - Create workloads using the pushed images
   - Environment variables: `DATABASE_URL` for backend
   - Port mappings: 8080 for backend, 3000 for frontend (containers expose these)
   - Health checks are configured in the images
   - (Docker ports 3001/8081 are only for local development to avoid conflicts)

## Environment Variables

**Backend:**
- `DATABASE_URL`: PostgreSQL connection string (required)

**Frontend:**
- `NODE_ENV`: Defaults to `production` in the image

## Local Development

To develop locally without Docker, use:
```bash
# Backend
cd backend && go run main.go

# Frontend (in another terminal)
cd frontend && npm run dev
```

## CI/CD

### GitHub Actions Workflow

The GitHub Actions workflow (`.github/workflows/ci.yml`) automatically:

1. **Tests & builds** on every push/PR
   - Backend: `go test`, compile check
   - Frontend: `npm build`, Prettier formatting check

2. **Builds and pushes Docker images** on push to `main` or `develop` branches
   - Registry: GitHub Container Registry (GHCR)
   - Images automatically tagged with `latest` and git SHA
   - Build cache enabled for faster subsequent builds
   - **No manual action required** — uses `GITHUB_TOKEN` automatically

### Image locations after push

After pushing to main/develop, images are available at:
```
ghcr.io/<your-username>/scheduling-backend:latest
ghcr.io/<your-username>/scheduling-backend:<git-sha>

ghcr.io/<your-username>/scheduling-frontend:latest
ghcr.io/<your-username>/scheduling-frontend:<git-sha>
```

### Using images in Rancher

1. **Make images public** (optional, for easy Rancher access):
   - Go to GitHub repo → Packages
   - Click each image → Package settings → Change visibility to Public

2. **Deploy in Rancher**:
   - Use image URL: `ghcr.io/<your-username>/scheduling-backend:latest`
   - For private images, create an image pull secret with GitHub token

### Alternative registries

To push to Docker Hub instead, see [`.github/workflows/publish-dockerhub.yml`](alternative setup available on request).

## Troubleshooting

**Backend won't start:** Ensure `DATABASE_URL` is set and PostgreSQL is running (check `docker-compose ps`)

**Frontend won't build:** Check `frontend/package-lock.json` exists (required for `npm ci`)

**Port conflicts:** Create `.env` file and change ports (don't edit docker-compose.yml):
```bash
cp .env.docker .env
# Edit FRONTEND_PORT, BACKEND_PORT, POSTGRES_PORT
docker-compose up --build
```

**Database seeding failed:** Verify PostgreSQL is healthy:
```bash
docker-compose ps  # should show postgres as "healthy"
docker logs scheduling-db  # check for errors
```
