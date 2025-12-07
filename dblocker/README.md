# DBLocker

A database lock manager for coordinating test database access in parallel testing scenarios.

## Installation

```bash
go install github.com/rickchristie/govner/dblocker@latest
```

## Usage

### First Run (Interactive Setup)

Run without arguments to start the interactive setup wizard:

```bash
dblocker
```

The wizard will prompt you for:
- Database host, port, username, password
- Database name prefix
- Number of test databases
- Where to save the config file

Example session:
```
DBLocker Setup
==============

Database host [localhost]: mydb.example.com
Database port [9090]: 5432
Database username [tester]:
Database password [LegacyCodeIsOneWithNoTest]: MySecurePassword
Database name prefix [tester]: testdb
Number of test databases (1-100) [25]: 50

Save config to [/home/user/.config/dblocker/config.json]:

Config saved to /home/user/.config/dblocker/config.json
Run with: dblocker --config "/home/user/.config/dblocker/config.json"
```

### Running with Config

Once you have a config file, run with:

```bash
dblocker --config ~/.config/dblocker/config.json
```

### Running in Background

Using nohup:
```bash
nohup dblocker --config ~/.config/dblocker/config.json > /var/log/dblocker.log 2>&1 &
```

Using systemd (create `/etc/systemd/system/dblocker.service`):
```ini
[Unit]
Description=DBLocker Service
After=network.target

[Service]
Type=simple
ExecStart=/usr/local/bin/dblocker --config /etc/dblocker/config.json
Restart=always
RestartSec=5

[Install]
WantedBy=multi-user.target
```

Then:
```bash
sudo systemctl daemon-reload
sudo systemctl enable dblocker
sudo systemctl start dblocker
```

## Configuration

Config file format (`config.json`):

```json
{
  "db_host": "localhost",
  "db_port": "9090",
  "db_username": "tester",
  "db_password": "LegacyCodeIsOneWithNoTest",
  "db_database_prefix": "tester",
  "test_db_count": 25
}
```

| Field | Default | Description |
|-------|---------|-------------|
| `db_host` | `localhost` | Database host |
| `db_port` | `9090` | Database port |
| `db_username` | `tester` | Database username |
| `db_password` | `LegacyCodeIsOneWithNoTest` | Database password |
| `db_database_prefix` | `tester` | Database name prefix |
| `test_db_count` | `25` | Number of test databases (max: 100) |

## API Endpoints

### Lock a Database

```bash
curl "http://localhost:9191/lock?username=YOUR_USERNAME&password=gotestyourcode"
```

Returns a PostgreSQL connection string. Blocks until a database is available.

### Unlock a Database

```bash
curl -X POST "http://localhost:9191/unlock?username=YOUR_USERNAME&password=gotestyourcode" \
  -d "postgresql://tester:xxx@localhost:9090/tester1"
```

## Admin Panel

- **URL**: http://localhost:9191/admin
- **Password**: `gotestyourcode`

Features:
- View all database locks
- Force unlock individual databases
- Unlock all databases by username

## Behavior

- Databases auto-unlock after 30 minutes of inactivity
- Admin sessions expire after 1 hour of inactivity
- Admin panel auto-refreshes every 30 seconds
- Status is logged every 5 minutes
- Maximum 100 test databases supported
- Requires test database server to be running

## Security Note

This service uses a simple hardcoded password and is intended for internal/VPN-protected environments only.
