#!/bin/bash

# Initialize parameters
cpu_param=""

# Set defaults if not specified
if [ -z "$TEST_DB_USAGE" ]; then
    TEST_DB_USAGE=25
fi

if [ -z "$POSTGRES_PORT" ]; then
    POSTGRES_PORT=9090
fi

# Calculate tmpfs size based on TEST_DB_USAGE
# Starts with 612m at 1-2, scaling up to 2148m at 25, and 3.6GB for higher usage
if [ "$TEST_DB_USAGE" -le 2 ]; then
    tmpfs_size="612m"
elif [ "$TEST_DB_USAGE" -le 5 ]; then
    tmpfs_size="868m"
elif [ "$TEST_DB_USAGE" -le 10 ]; then
    tmpfs_size="1124m"
elif [ "$TEST_DB_USAGE" -le 15 ]; then
    tmpfs_size="1380m"
elif [ "$TEST_DB_USAGE" -le 20 ]; then
    tmpfs_size="1636m"
elif [ "$TEST_DB_USAGE" -le 25 ]; then
    tmpfs_size="2148m"
else
    tmpfs_size="3686m"
fi

# Parse command line arguments
while [[ $# -gt 0 ]]; do
    case $1 in
        -cpu)
            cpu_param="--cpus=$2"
            shift 2
            ;;
        -port)
            POSTGRES_PORT="$2"
            shift 2
            ;;
        *)
            shift
            ;;
    esac
done

echo ">>> STOP CURRENTLY RUNNING CONTAINER <<<"
docker stop govner-testdb-ct
docker stop govner-dblocker-ct
docker system prune -f

# See: https://hub.docker.com/_/postgres
echo ">>> RUN CONTAINER <<<"
echo ">>> TEST_DB_USAGE=$TEST_DB_USAGE <<<"
echo ">>> POSTGRES_PORT=$POSTGRES_PORT <<<"
echo ">>> tmpfs_size=$tmpfs_size <<<"
echo ">>> cpu_param=$cpu_param <<<"
docker run -d --name govner-testdb-ct $cpu_param --net=host -e NUM_TEST_DBS=$TEST_DB_USAGE --tmpfs /var/lib/postgresql/data:rw,noexec,nosuid,size=$tmpfs_size govner-testdb:12 -p ${POSTGRES_PORT} -c 'config_file=/etc/postgresql/postgresql.conf'

# Wait until postgres database is ready to be used.
until pg_isready -h 127.0.0.1 -p ${POSTGRES_PORT} -U tester; do
    echo "Waiting for PostgreSQL on port ${POSTGRES_PORT} to be ready..."
    sleep 1
done
echo "PostgreSQL is ready on port ${POSTGRES_PORT}"
