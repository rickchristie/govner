# Use the official PostgreSQL image from the Docker Hub as the base image
# See: https://hub.docker.com/_/postgres
FROM postgres:15

# Set environment variables if needed (these are optional)
ENV POSTGRES_DB=postgres
ENV POSTGRES_USER=postgres
ENV POSTGRES_PASSWORD=LegacyCodeIsOneWithNoTest

# Copy your custom configuration file into the container
COPY ./postgresql.conf /etc/postgresql/postgresql.conf

# Set the PostgreSQL configuration file environment variable
ENV POSTGRES_CONFIG_FILE=/etc/postgresql/postgresql.conf

# Install PostGIS
# See: https://github.com/postgis/docker-postgis/blob/81a0b55135c13f23045e1504b5e78239fe0df3fa/14-3.2/Dockerfile
ENV POSTGIS_MAJOR=3

# TODO: When execute, please check the current version of the postgis.
ENV POSTGIS_VERSION=3.6.1+dfsg-1~exp1.pgdg13+1

# Shows available PostGIS package version for installation.
RUN apt-cache showpkg postgresql-$PG_MAJOR-postgis-$POSTGIS_MAJOR

# When the script is run, if the POSTGIS_VERSION is not found, it will show the latest version when running this step.
# see the dockerfile build log, then update the POSTGIS_VERSION variable. And it should be fine.
# It will look like this:
# 8 [4/5] RUN apt-get update       && apt-cache showpkg postgresql-13-postgis-3       && apt-get install -y --no-install-recommends            postgresql-13-postgis-3=3.5.3+dfsg-1~exp1.pgdg120+1            postgresql-13-postgis-3-scripts       && rm -rf /var/lib/apt/lists/*
# 8 0.406 Get:1 http://deb.debian.org/debian bookworm InRelease [151 kB]
# 8 1.851 Fetched 9,847 kB in 2s (6,240 kB/s)
# 8 1.851 Reading package lists...
# 8 2.388 Package: postgresql-13-postgis-3
# 8 2.388 Versions:
# 8 2.388 3.5.3+dfsg-1~exp1.pgdg120+1 (/var/lib/apt/lists/apt.postgresql.org_pub_repos_apt_dists_bookworm-pgdg_main_binary-amd64_Packages.lz4)
# the lines after 'Versions:' is the version that you need to update in the POSTGIS_VERSION variable.
RUN apt-get update \
      && apt-cache showpkg postgresql-$PG_MAJOR-postgis-$POSTGIS_MAJOR \
      && apt-get install -y --no-install-recommends \
           postgresql-$PG_MAJOR-postgis-$POSTGIS_MAJOR=$POSTGIS_VERSION \
           postgresql-$PG_MAJOR-postgis-$POSTGIS_MAJOR-scripts \
      && rm -rf /var/lib/apt/lists/*

## The default port that PostgreSQL listens on is already exposed in the base image, but you can restate it if you like.
EXPOSE 9090

# You can add custom initialization scripts in docker-entrypoint-initdb.d.
COPY ./init.sh /docker-entrypoint-initdb.d/

# When the container starts, PostgreSQL will execute any *.sql or *.sh scripts found in this directory.