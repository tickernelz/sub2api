# Sub2API Docker Image

Sub2API is an AI API Gateway Platform for distributing and managing AI product subscription API quotas.

Image: `tickernelz/sub2api`

Supported architectures:

- `linux/amd64`
- `linux/arm64`

## Recommended: Docker Compose

Docker Compose is the recommended installation method because Sub2API needs the app container plus PostgreSQL and Redis.

### One-Click Setup

```bash
mkdir -p sub2api-deploy && cd sub2api-deploy
curl -sSL https://raw.githubusercontent.com/tickernelz/sub2api/main/deploy/docker-deploy.sh | bash
docker compose up -d
```

Check logs:

```bash
docker compose logs -f sub2api
```

Open the web UI:

```txt
http://YOUR_SERVER_IP:8080
```

If the admin password was auto-generated, find it in logs:

```bash
docker compose logs sub2api | grep "admin password"
```

## Manual Docker Compose

Use this when you want to manage the compose file and `.env` yourself.

```yaml
services:
  sub2api:
    image: tickernelz/sub2api:0.1.132
    container_name: sub2api
    restart: unless-stopped
    ports:
      - "8080:8080"
    volumes:
      - ./data:/app/data
    environment:
      - AUTO_SETUP=true
      - SERVER_HOST=0.0.0.0
      - SERVER_PORT=8080
      - DATABASE_HOST=postgres
      - DATABASE_PORT=5432
      - DATABASE_USER=sub2api
      - DATABASE_PASSWORD=${POSTGRES_PASSWORD}
      - DATABASE_DBNAME=sub2api
      - DATABASE_SSLMODE=disable
      - REDIS_HOST=redis
      - REDIS_PORT=6379
      - REDIS_PASSWORD=${REDIS_PASSWORD:-}
      - REDIS_DB=0
      - JWT_SECRET=${JWT_SECRET}
      - TOTP_ENCRYPTION_KEY=${TOTP_ENCRYPTION_KEY}
      - ADMIN_EMAIL=${ADMIN_EMAIL:-admin@sub2api.local}
      - ADMIN_PASSWORD=${ADMIN_PASSWORD:-}
    depends_on:
      postgres:
        condition: service_healthy
      redis:
        condition: service_healthy

  postgres:
    image: postgres:18-alpine
    container_name: sub2api-postgres
    restart: unless-stopped
    environment:
      - POSTGRES_USER=sub2api
      - POSTGRES_PASSWORD=${POSTGRES_PASSWORD}
      - POSTGRES_DB=sub2api
    volumes:
      - ./postgres_data:/var/lib/postgresql/data
    healthcheck:
      test: ["CMD-SHELL", "pg_isready -U sub2api -d sub2api"]
      interval: 10s
      timeout: 5s
      retries: 5

  redis:
    image: redis:8-alpine
    container_name: sub2api-redis
    restart: unless-stopped
    volumes:
      - ./redis_data:/data
    command: >
      sh -c 'if [ -n "$${REDIS_PASSWORD}" ]; then redis-server --appendonly yes --requirepass "$${REDIS_PASSWORD}"; else redis-server --appendonly yes; fi'
    healthcheck:
      test: ["CMD", "redis-cli", "ping"]
      interval: 10s
      timeout: 5s
      retries: 5
```

Create `.env`:

```bash
POSTGRES_PASSWORD=<secure-postgres-password>
JWT_SECRET=<secure-random-hex>
TOTP_ENCRYPTION_KEY=<secure-random-hex>

# Optional
REDIS_PASSWORD=
ADMIN_EMAIL=admin@example.com
ADMIN_PASSWORD=
```

Generate secrets with:

```bash
openssl rand -hex 32
```

Start:

```bash
mkdir -p data postgres_data redis_data
docker compose up -d
```

## Advanced: Docker Run with Existing PostgreSQL and Redis

Use `docker run` when PostgreSQL and Redis already exist. For a full single-server deployment, use Docker Compose instead.

```bash
docker run -d \
  --name sub2api \
  --restart unless-stopped \
  -p 8080:8080 \
  -v sub2api_data:/app/data \
  -e AUTO_SETUP=true \
  -e SERVER_HOST=0.0.0.0 \
  -e SERVER_PORT=8080 \
  -e DATABASE_HOST=postgres.example.internal \
  -e DATABASE_PORT=5432 \
  -e DATABASE_USER=sub2api \
  -e DATABASE_PASSWORD='<secure-postgres-password>' \
  -e DATABASE_DBNAME=sub2api \
  -e DATABASE_SSLMODE=disable \
  -e REDIS_HOST=redis.example.internal \
  -e REDIS_PORT=6379 \
  -e REDIS_PASSWORD='' \
  -e REDIS_DB=0 \
  -e JWT_SECRET='<secure-random-hex>' \
  -e TOTP_ENCRYPTION_KEY='<secure-random-hex>' \
  -e ADMIN_EMAIL=admin@example.com \
  tickernelz/sub2api:0.1.132
```

## Tags

- `latest` - latest stable release
- `x.y.z` - exact release, recommended for production
- `x.y` - latest patch of a minor version
- `x` - latest minor of a major version

Use `latest` for quick testing. Pin a version tag such as `0.1.132` for production.

## Links

- [GitHub Repository](https://github.com/tickernelz/sub2api)
- [Documentation](https://github.com/tickernelz/sub2api#readme)
