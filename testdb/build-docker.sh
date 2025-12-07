#!/bin/bash
echo ">>> STOP CURRENTLY RUNNING CONTAINERS <<<"
docker stop govner-testdb-ct
docker system prune -f

echo ">>> DELETING IMAGES <<<"
# For govner-testdb
for IMAGE in $(docker images govner-testdb --format "{{.Repository}}:{{.Tag}}"); do
    docker rmi "$IMAGE" || true
done

echo ">>> BUILDING IMAGES <<<"
docker build --progress=plain --no-cache -t govner-testdb:12 .

echo ">>> CLEAN-UP DOCKER <<<"
docker system prune -f

echo ">>> DONE <<<"
docker images