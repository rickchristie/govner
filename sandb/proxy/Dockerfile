# AI Sandbox Squid Proxy
# Provides network whitelisting for the CLI container
#
# Features:
# - Squid HTTP/HTTPS proxy with domain whitelisting
# - Log rotation to prevent disk fill
# - Optional: socat for host service access (see below)

FROM alpine:3.19

# Install squid and utilities
RUN apk add --no-cache squid logrotate bash

# ============================================================================
# OPTIONAL: Add tools for advanced setups
# ============================================================================
# Uncomment to enable additional features:
#
# socat: For proxying services from container to host
# docker-cli: For managing containers from proxy (e.g., test DB helper)
# postgresql-client: For database connectivity checks
# nmap-ncat: For TCP service helpers
#
# RUN apk add --no-cache socat docker-cli nmap-ncat postgresql-client
# ============================================================================

COPY squid.conf /etc/squid/squid.conf

# Ensure log directory exists and is writable by squid
RUN mkdir -p /var/log/squid && chown squid:squid /var/log/squid

# Logrotate config - rotate at 10MB, keep 2 rotations
RUN echo '/var/log/squid/*.log {\n\
    size 10M\n\
    rotate 2\n\
    missingok\n\
    notifempty\n\
    compress\n\
    delaycompress\n\
    postrotate\n\
        squid -k rotate 2>/dev/null || true\n\
    endscript\n\
}' > /etc/logrotate.d/squid

# ============================================================================
# OPTIONAL: Add custom helper scripts
# ============================================================================
# Example: A TCP service that manages test databases
# COPY testdb-helper.sh /usr/local/bin/testdb-helper.sh
# RUN chmod +x /usr/local/bin/testdb-helper.sh
# EXPOSE 9292
# ============================================================================

EXPOSE 3128

# Run squid, periodic logrotate (every hour), and tail logs
# Remove stale PID file on startup (fixes restart after crash)
CMD ["sh", "-c", "rm -f /var/run/squid.pid && squid -N & while true; do sleep 3600; logrotate /etc/logrotate.d/squid; done & tail -F /var/log/squid/access.log /var/log/squid/cache.log 2>/dev/null"]
