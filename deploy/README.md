# Sub2API Deployment Files

This directory contains deployment files for Sub2API on Linux servers.

## Deployment Methods

| Method | Best For | Notes |
|--------|----------|-------|
| **Docker Compose** | Recommended all-in-one server setup | Runs Sub2API + PostgreSQL + Redis |
| **Docker Run** | Advanced users with existing PostgreSQL/Redis | App container only |
| **Binary Install** | Bare-metal/systemd deployments | Requires PostgreSQL and Redis installed separately |

## Files

| File | Description |
|------|-------------|
| `docker-compose.yml` | Docker Compose configuration using named volumes |
| `docker-compose.local.yml` | Docker Compose configuration using local directories, recommended for migration/backups |
| `docker-deploy.sh` | One-click Docker deployment preparation script |
| `.env.example` | Docker environment variables template |
| `DOCKER.md` | Docker Hub / image usage documentation |
| `install.sh` | One-click binary installation script |
| `install-datamanagementd.sh` | datamanagementd 一键安装脚本 |
| `sub2api.service` | Systemd service unit file |
| `sub2api-datamanagementd.service` | datamanagementd systemd service unit file |
| `DATAMANAGEMENTD_CN.md` | datamanagementd 部署与联动说明（中文） |
| `config.example.yaml` | Example configuration file |

---

## Docker Compose Deployment (Recommended)

Docker Compose is the recommended deployment path. It runs Sub2API with PostgreSQL and Redis, persists data, and is easier to upgrade, backup, and migrate than a long multi-container `docker run` setup.

### One-Click Setup

```bash
mkdir -p sub2api-deploy && cd sub2api-deploy
curl -sSL https://raw.githubusercontent.com/tickernelz/sub2api/main/deploy/docker-deploy.sh | bash
docker compose up -d
```

The script:

- downloads `docker-compose.local.yml` as `docker-compose.yml`
- downloads `.env.example`
- generates `POSTGRES_PASSWORD`, `JWT_SECRET`, and `TOTP_ENCRYPTION_KEY`
- creates `.env`, `data/`, `postgres_data/`, and `redis_data/`
- uses the Docker image `tickernelz/sub2api:latest`

Check status:

```bash
docker compose ps
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

### Manual Docker Compose

```bash
git clone https://github.com/tickernelz/sub2api.git
cd sub2api/deploy

cp .env.example .env
nano .env

openssl rand -hex 32  # POSTGRES_PASSWORD
openssl rand -hex 32  # JWT_SECRET
openssl rand -hex 32  # TOTP_ENCRYPTION_KEY

mkdir -p data postgres_data redis_data
docker compose -f docker-compose.local.yml up -d
```

Required/recommended `.env` values:

```bash
POSTGRES_PASSWORD=<secure-postgres-password>
JWT_SECRET=<secure-random-hex>
TOTP_ENCRYPTION_KEY=<secure-random-hex>

# Optional
SERVER_PORT=8080
ADMIN_EMAIL=admin@example.com
ADMIN_PASSWORD=<optional-initial-admin-password>
```

### Production Image Tag

`latest` is convenient for quick testing. For production, pin a release tag so upgrades are deliberate:

```yaml
services:
  sub2api:
    image: tickernelz/sub2api:0.1.132
```

### Compose Variants

| File | Data Storage | Migration | Best For |
|------|--------------|-----------|----------|
| `docker-compose.local.yml` | Local directories: `./data`, `./postgres_data`, `./redis_data` | Easy: archive the deployment directory | Production, backups, migration |
| `docker-compose.yml` | Docker named volumes | Requires Docker volume commands | Simple setup |
| `docker-compose.standalone.yml` | App container only | Depends on external services | Existing PostgreSQL/Redis |

### Common Docker Compose Commands

```bash
# Start
docker compose -f docker-compose.local.yml up -d

# Stop
docker compose -f docker-compose.local.yml down

# Restart Sub2API only
docker compose -f docker-compose.local.yml restart sub2api

# View logs
docker compose -f docker-compose.local.yml logs -f sub2api

# Update image and recreate containers
docker compose -f docker-compose.local.yml pull
docker compose -f docker-compose.local.yml up -d
```

### Easy Migration

```bash
# On source server
cd /path/to/sub2api-deploy
docker compose down
cd ..
tar czf sub2api-deploy.tar.gz sub2api-deploy/

# On new server
tar xzf sub2api-deploy.tar.gz
cd sub2api-deploy/
docker compose up -d
```

Your configuration and data move together when using the local-directory compose file.

## Docker Run with Existing PostgreSQL and Redis (Advanced)

Use `docker run` only when PostgreSQL and Redis already exist. For a full single-server deployment, use Docker Compose instead.

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

---

## Gemini OAuth Configuration

Sub2API supports three methods to connect to Gemini:

### Method 1: Code Assist OAuth (Recommended for GCP Users)

**No configuration needed** - always uses the built-in Gemini CLI OAuth client (public).

1. Leave `GEMINI_OAUTH_CLIENT_ID` and `GEMINI_OAUTH_CLIENT_SECRET` empty
2. In the Admin UI, create a Gemini OAuth account and select **"Code Assist"** type
3. Complete the OAuth flow in your browser

> Note: Even if you configure `GEMINI_OAUTH_CLIENT_ID` / `GEMINI_OAUTH_CLIENT_SECRET` for AI Studio OAuth,
> Code Assist OAuth will still use the built-in Gemini CLI client.

**Requirements:**
- Google account with access to Google Cloud Platform
- A GCP project (auto-detected or manually specified)

**How to get Project ID (if auto-detection fails):**
1. Go to [Google Cloud Console](https://console.cloud.google.com/)
2. Click the project dropdown at the top of the page
3. Copy the Project ID (not the project name) from the list
4. Common formats: `my-project-123456` or `cloud-ai-companion-xxxxx`

### Method 2: AI Studio OAuth (For Regular Google Accounts)

Requires your own OAuth client credentials.

**Step 1: Create OAuth Client in Google Cloud Console**

1. Go to [Google Cloud Console - Credentials](https://console.cloud.google.com/apis/credentials)
2. Create a new project or select an existing one
3. **Enable the Generative Language API:**
   - Go to "APIs & Services" → "Library"
   - Search for "Generative Language API"
   - Click "Enable"
4. **Configure OAuth Consent Screen** (if not done):
   - Go to "APIs & Services" → "OAuth consent screen"
   - Choose "External" user type
   - Fill in app name, user support email, developer contact
   - Add scopes: `https://www.googleapis.com/auth/generative-language.retriever` (and optionally `https://www.googleapis.com/auth/cloud-platform`)
   - Add test users (your Google account email)
5. **Create OAuth 2.0 credentials:**
   - Go to "APIs & Services" → "Credentials"
   - Click "Create Credentials" → "OAuth client ID"
   - Application type: **Web application** (or **Desktop app**)
   - Name: e.g., "Sub2API Gemini"
   - Authorized redirect URIs: Add `http://localhost:1455/auth/callback`
6. Copy the **Client ID** and **Client Secret**
7. **⚠️ Publish to Production (IMPORTANT):**
   - Go to "APIs & Services" → "OAuth consent screen"
   - Click "PUBLISH APP" to move from Testing to Production
   - **Testing mode limitations:**
     - Only manually added test users can authenticate (max 100 users)
     - Refresh tokens expire after 7 days
     - Users must be re-added periodically
   - **Production mode:** Any Google user can authenticate, tokens don't expire
   - Note: For sensitive scopes, Google may require verification (demo video, privacy policy)

**Step 2: Configure Environment Variables**

```bash
GEMINI_OAUTH_CLIENT_ID=your-client-id.apps.googleusercontent.com
GEMINI_OAUTH_CLIENT_SECRET=GOCSPX-your-client-secret

# 可选：如需使用 Gemini CLI 内置 OAuth Client（Code Assist / Google One）
# 安全说明：本仓库不会内置该 client_secret，请在运行环境通过环境变量注入。
# GEMINI_CLI_OAUTH_CLIENT_SECRET=GOCSPX-your-built-in-secret
```

**Step 3: Create Account in Admin UI**

1. Create a Gemini OAuth account and select **"AI Studio"** type
2. Complete the OAuth flow
   - After consent, your browser will be redirected to `http://localhost:1455/auth/callback?code=...&state=...`
   - Copy the full callback URL (recommended) or just the `code` and paste it back into the Admin UI

### Method 3: API Key (Simplest)

1. Go to [Google AI Studio](https://aistudio.google.com/app/apikey)
2. Click "Create API key"
3. In Admin UI, create a Gemini **API Key** account
4. Paste your API key (starts with `AIza...`)

### Comparison Table

| Feature | Code Assist OAuth | AI Studio OAuth | API Key |
|---------|-------------------|-----------------|---------|
| Setup Complexity | Easy (no config) | Medium (OAuth client) | Easy |
| GCP Project Required | Yes | No | No |
| Custom OAuth Client | No (built-in) | Yes (required) | N/A |
| Rate Limits | GCP quota | Standard | Standard |
| Best For | GCP developers | Regular users needing OAuth | Quick testing |

---

## Binary Installation

For production servers using systemd.

### One-Line Installation

```bash
curl -sSL https://raw.githubusercontent.com/tickernelz/sub2api/main/deploy/install.sh | sudo bash
```

### Manual Installation

1. Download the latest release from [GitHub Releases](https://github.com/tickernelz/sub2api/releases)
2. Extract and copy the binary to `/opt/sub2api/`
3. Copy `sub2api.service` to `/etc/systemd/system/`
4. Run:
   ```bash
   sudo systemctl daemon-reload
   sudo systemctl enable sub2api
   sudo systemctl start sub2api
   ```
5. Open the Setup Wizard in your browser to complete configuration

### Commands

```bash
# Install
sudo ./install.sh

# Upgrade
sudo ./install.sh upgrade

# Uninstall
sudo ./install.sh uninstall
```

### Service Management

```bash
# Start the service
sudo systemctl start sub2api

# Stop the service
sudo systemctl stop sub2api

# Restart the service
sudo systemctl restart sub2api

# Check status
sudo systemctl status sub2api

# View logs
sudo journalctl -u sub2api -f

# Enable auto-start on boot
sudo systemctl enable sub2api
```

### Configuration

#### Server Address and Port

During installation, you will be prompted to configure the server listen address and port. These settings are stored in the systemd service file as environment variables.

To change after installation:

1. Edit the systemd service:
   ```bash
   sudo systemctl edit sub2api
   ```

2. Add or modify:
   ```ini
   [Service]
   Environment=SERVER_HOST=0.0.0.0
   Environment=SERVER_PORT=3000
   ```

3. Reload and restart:
   ```bash
   sudo systemctl daemon-reload
   sudo systemctl restart sub2api
   ```

#### Gemini OAuth Configuration

If you need to use AI Studio OAuth for Gemini accounts, add the OAuth client credentials to the systemd service file:

1. Edit the service file:
   ```bash
   sudo nano /etc/systemd/system/sub2api.service
   ```

2. Add your OAuth credentials in the `[Service]` section (after the existing `Environment=` lines):
   ```ini
   Environment=GEMINI_OAUTH_CLIENT_ID=your-client-id.apps.googleusercontent.com
   Environment=GEMINI_OAUTH_CLIENT_SECRET=GOCSPX-your-client-secret
   ```

   如需使用“内置 Gemini CLI OAuth Client”（Code Assist / Google One），还需要注入：
   ```ini
   Environment=GEMINI_CLI_OAUTH_CLIENT_SECRET=GOCSPX-your-built-in-secret
   ```

3. Reload and restart:
   ```bash
   sudo systemctl daemon-reload
   sudo systemctl restart sub2api
   ```

> **Note:** Code Assist OAuth does not require any configuration - it uses the built-in Gemini CLI client.
> See the [Gemini OAuth Configuration](#gemini-oauth-configuration) section above for detailed setup instructions.

#### Application Configuration

The main config file is at `/etc/sub2api/config.yaml` (created by Setup Wizard).

### Prerequisites

- Linux server (Ubuntu 20.04+, Debian 11+, CentOS 8+, etc.)
- PostgreSQL 14+
- Redis 6+
- systemd

### Directory Structure

```
/opt/sub2api/
├── sub2api              # Main binary
├── sub2api.backup       # Backup (after upgrade)
└── data/                # Runtime data

/etc/sub2api/
└── config.yaml          # Configuration file
```

---

## Troubleshooting

### Docker

For **local directory version**:

```bash
# Check container status
docker compose -f docker-compose.local.yml ps

# View detailed logs
docker compose -f docker-compose.local.yml logs --tail=100 sub2api

# Check database connection
docker compose -f docker-compose.local.yml exec postgres pg_isready

# Check Redis connection
docker compose -f docker-compose.local.yml exec redis redis-cli ping

# Restart all services
docker compose -f docker-compose.local.yml restart

# Check data directories
ls -la data/ postgres_data/ redis_data/
```

For **named volumes version**:

```bash
# Check container status
docker compose ps

# View detailed logs
docker compose logs --tail=100 sub2api

# Check database connection
docker compose exec postgres pg_isready

# Check Redis connection
docker compose exec redis redis-cli ping

# Restart all services
docker compose restart
```

### Binary Install

```bash
# Check service status
sudo systemctl status sub2api

# View recent logs
sudo journalctl -u sub2api -n 50

# Check config file
sudo cat /etc/sub2api/config.yaml

# Check PostgreSQL
sudo systemctl status postgresql

# Check Redis
sudo systemctl status redis
```

### Common Issues

1. **Port already in use**: Change `SERVER_PORT` in `.env` or systemd config
2. **Database connection failed**: Check PostgreSQL is running and credentials are correct
3. **Redis connection failed**: Check Redis is running and password is correct
4. **Permission denied**: Ensure proper file ownership for binary install

---

## TLS Fingerprint Configuration

Sub2API supports TLS fingerprint simulation to make requests appear as if they come from the official Claude CLI (Node.js client).

> **💡 Tip:** Visit **[tls.sub2api.org](https://tls.sub2api.org/)** to get TLS fingerprint information for different devices and browsers.

### Default Behavior

- Built-in `claude_cli_v2` profile simulates Node.js 20.x + OpenSSL 3.x
- JA3 Hash: `1a28e69016765d92e3b381168d68922c`
- JA4: `t13d5911h1_a33745022dd6_1f22a2ca17c4`
- Profile selection: `accountID % profileCount`

### Configuration

```yaml
gateway:
  tls_fingerprint:
    enabled: true  # Global switch
    profiles:
      # Simple profile (uses default cipher suites)
      profile_1:
        name: "Profile 1"

      # Profile with custom cipher suites (use compact array format)
      profile_2:
        name: "Profile 2"
        cipher_suites: [4866, 4867, 4865, 49199, 49195, 49200, 49196]
        curves: [29, 23, 24]
        point_formats: 0

      # Another custom profile
      profile_3:
        name: "Profile 3"
        cipher_suites: [4865, 4866, 4867, 49199, 49200]
        curves: [29, 23, 24, 25]
```

### Profile Fields

| Field | Type | Description |
|-------|------|-------------|
| `name` | string | Display name (required) |
| `cipher_suites` | []uint16 | Cipher suites in decimal. Empty = default |
| `curves` | []uint16 | Elliptic curves in decimal. Empty = default |
| `point_formats` | []uint8 | EC point formats. Empty = default |

### Common Values Reference

**Cipher Suites (TLS 1.3):** `4865` (AES_128_GCM), `4866` (AES_256_GCM), `4867` (CHACHA20)

**Cipher Suites (TLS 1.2):** `49195`, `49196`, `49199`, `49200` (ECDHE variants)

**Curves:** `29` (X25519), `23` (P-256), `24` (P-384), `25` (P-521)
