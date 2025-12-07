# Install Docker

### Install in Ubuntu

Follow instructions here: [Install docker engine in Ubuntu](https://docs.docker.com/engine/install/ubuntu/). In short,
we need to do the following.

Uninstall previous versions:

```shell
for pkg in docker.io docker-doc docker-compose docker-compose-v2 podman-docker containerd runc; do sudo apt-get remove $pkg; done
```

Install using `apt` repository:

```shell
# Add Docker's official GPG key:
sudo apt-get update
sudo apt-get install ca-certificates curl
sudo install -m 0755 -d /etc/apt/keyrings
sudo curl -fsSL https://download.docker.com/linux/ubuntu/gpg -o /etc/apt/keyrings/docker.asc
sudo chmod a+r /etc/apt/keyrings/docker.asc


# Add the repository to Apt sources:
echo \
  "deb [arch=$(dpkg --print-architecture) signed-by=/etc/apt/keyrings/docker.asc] https://download.docker.com/linux/ubuntu \
  $(. /etc/os-release && echo "$VERSION_CODENAME") stable" | \
  sudo tee /etc/apt/sources.list.d/docker.list > /dev/null
sudo apt-get update
```

Install docker:

```shell
sudo apt-get install docker-ce docker-ce-cli containerd.io docker-buildx-plugin docker-compose-plugin
```

### Check that installation is successful

Run `hello-world` to check that installation is successful:

```shell
sudo docker run hello-world
```

### Allow running docker command without `sudo`

```shell
sudo usermod -a -G docker $USER
```

Restart your PC (you have to restart, otherwise it won't be loaded), then check your user's groups and ensure `docker`
is listed:

```shell
groups
```

Then Check if the user can run this command:

```shell
docker ps
```

### Build and run scripts

The Docker image is built once and can be run with different configurations.

**Build the image** (only needed once):
```shell
./test-db-docker-build.sh
```

**Run with default configuration** (25 databases):
```shell
./test-db-docker-run.sh
```

**Run with custom number of databases**:
```shell
TEST_DB_USAGE=50 ./test-db-docker-run.sh
```

The number of test databases is configured at **runtime** via the `NUM_TEST_DBS` environment variable (default: 25), so you don't need to rebuild the image to change the database count.



# Documentation

### Ensure that docker daemon is running

Then ensure that docker daemon is up and running:

```shell
sudo systemctl status docker
```

You can start, stop, or restart docker daemon service with:

```shell
sudo systemctl status docker
sudo systemctl stop docker
sudo systemctl start docker
sudo systemctl restart docker
```

There's usually no reason to do this. Documented here just in case.

### Modifying `Dockerfile` to install PostGIS

The `apt-cache` command actually prints out the version that's available for install, so we can print them.
We can't actually use exact version string installed in Ubuntu, because docker is using a different kind of debian OS.

```shell
ENV POSTGIS_MAJOR=3
RUN apt-cache showpkg postgresql-$PG_MAJOR-postgis-$POSTGIS_MAJOR
```

### Postgres configuration

For testing purposes, we don't need durability in crashes, because each time the process crash (if they do), we can just
re-run the test again. Our fixtures are written in code so there's no need to persist anything. Therefore, to eliminate
I/O bottleneck, we follow [Postgres Non-Durability Setting](https://www.postgresql.org/docs/current/non-durability.html):

* We place database cluster's data directory in memory backed file-system, using Docker's `tmpfs`, thereby bypassing
  all I/O related wait. Even though SSD are fast, the operation are still very slow compared to memory. On each write,
  the TRIM operation actually has to be done, so SSD-heavy testing also degrade our hard-drive's performance over time.
* Turn off `fsync`.
* Turn off `synchronous_commit`.
* Turn off `full_page_writes`.

Other than the above, we also configured the following:

* `max_parallel_maintenance_workers` increases the amount of workers to create table, index, etc., which is what we
  would be doing a lot, as we're setting up new database each time a test is run.
* `max_parallel_workers` increased as well to support both maintenance workers and other queries.
* `maintenance_work_mem` increased to 1GB, to increase maintenance worker performance.
* `max_wal_senders` is set to 0, because we don't need to replicate our testing databases.
* `wal_level` set to `minimal`, to further reduce the amount of WAL that we need to process. Since WAL is used to
  recover from crashes, and we don't need data durability, we don't need it.
* `max_connections` is set to 5000, because we're going to run multiple tests in parallel.
* `max_locks_per_transaction` is set to 1024, because we're going to do plenty of locks due to the large amount of DDL
  statements we're going to process.

In the future we might be able to improve performance further by configuring WAL size, checkpoint timing, and background
writer settings. However, in preliminary testing, it seems the bottleneck has changed to the processor, so there's no
need to go that route yet.

### Networking

Using default bridge networking results in connection errors. Subsequent tests using host networking removes these
errors, so don't change it back to using bridge networking.

### Docker logs

We can actually use this command to get the logs of the docker instance.

```shell
docker logs --follow --tail 100 govner-testdb-ct
```

### Access database manually

We can access the database manually just like any other postgres instance:

```shell
psql -h localhost -p 9090 -U tester
```

We can access terminal on the docker instance with:

```shell
sudo docker exec -it govner-testdb-ct /bin/bash
```

# Resources

- [DockerHub Postgres Official Image Docs](https://hub.docker.com/_/postgres)
- [Postgres Non-Durability Setting](https://www.postgresql.org/docs/current/non-durability.html)
- [`tmpfs` mounts](https://docs.docker.com/storage/tmpfs/)