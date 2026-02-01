up:
	@echo "Starting Docker images..."
	docker-compose up -d
	@echo "Docker images started!"

down:
	@echo "Stopping Docker images..."
	docker-compose down
	@echo "Docker images stopped!"

## up_build: stops docker-compose (if running), builds all projects and starts docker compose
up_build:
	@echo "Stopping docker images (if running...)"
	docker-compose down
	@echo "Building (when required) and starting docker images..."
	docker-compose up --build -d
	@echo "Docker images built and started!"