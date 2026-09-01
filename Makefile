.PHONY: docker-up docker-down docker-logs docker-reseed docker-status

# Docker commands (alternative to docker-compose)
docker-up:
	docker-compose up -d
	@echo "✓ Services running on:"
	@echo "  Frontend: http://localhost:3001"
	@echo "  Backend:  http://localhost:8081"
	@echo "  Postgres: localhost:5433"

docker-down:
	docker-compose down

docker-logs:
	docker-compose logs -f

docker-status:
	docker-compose ps

docker-reseed:
	bash scripts/reseed.sh

docker-build:
	docker-compose build

docker-rebuild:
	docker-compose down
	docker-compose up --build -d
	@echo "✓ Rebuilt and running"

# Quick access
reseed: docker-reseed

.DEFAULT_GOAL := help

help:
	@echo "Docker Commands:"
	@echo "  make docker-up       - Start services"
	@echo "  make docker-down     - Stop services"
	@echo "  make docker-status   - Show status"
	@echo "  make docker-logs     - Follow logs"
	@echo "  make docker-reseed   - Re-seed database (option 11)"
	@echo "  make docker-build    - Build images"
	@echo "  make docker-rebuild  - Rebuild from scratch"
	@echo ""
	@echo "Local dev (manage.sh) still works as before:"
	@echo "  ./manage.sh          - Interactive menu"
