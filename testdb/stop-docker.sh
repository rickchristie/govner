#!/bin/bash

echo ">>> STOP CURRENTLY RUNNING CONTAINER <<<"
docker stop govner-testdb-ct
docker system prune -f