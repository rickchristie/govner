#!/bin/bash
# Use NUM_TEST_DBS environment variable, default to 25 if not set
NUM_DBS=${NUM_TEST_DBS:-25}
echo "Setup ${NUM_DBS} tester databases"

cd /var/lib/postgresql || exit

echo 'Creating user... (ignore role already exist error for re-runs)'
psql -c "CREATE USER tester WITH PASSWORD 'LegacyCodeIsOneWithNoTest' CREATEDB;"
psql -c "ALTER ROLE tester SUPERUSER;"
psql -c "CREATE DATABASE tester WITH ENCODING 'UTF8' LC_COLLATE='en_US.UTF-8' LC_CTYPE='en_US.UTF-8' TEMPLATE=template0;"
psql -c "ALTER DATABASE tester OWNER TO tester;"
psql -d tester -c "ALTER SCHEMA public OWNER TO tester;"
echo 'User creation done!'

PGPASSWORD=LegacyCodeIsOneWithNoTest psql -U tester -c "CREATE DATABASE test_template WITH ENCODING 'UTF8' LC_COLLATE='en_US.UTF-8' LC_CTYPE='en_US.UTF-8' TEMPLATE=template0;"
PGPASSWORD=LegacyCodeIsOneWithNoTest psql -U tester -d test_template -c 'CREATE EXTENSION IF NOT EXISTS postgis;'
PGPASSWORD=LegacyCodeIsOneWithNoTest psql -U tester -d test_template -c 'CREATE EXTENSION IF NOT EXISTS earthdistance CASCADE;'
PGPASSWORD=LegacyCodeIsOneWithNoTest psql -U tester -d test_template -c 'CREATE EXTENSION IF NOT EXISTS pg_trgm;'
PGPASSWORD=LegacyCodeIsOneWithNoTest psql -U tester -d test_template -c 'CREATE EXTENSION IF NOT EXISTS btree_gist;'
PGPASSWORD=LegacyCodeIsOneWithNoTest psql -U tester -d test_template -c 'VACUUM FREEZE;'
PGPASSWORD=LegacyCodeIsOneWithNoTest psql -U tester -d test_template -c "UPDATE pg_database SET datistemplate = TRUE WHERE datname = 'test_template';"

echo 'Database creation... (ignore already exist error for re-runs)'
for i in $(seq 1 ${NUM_DBS})
do
  echo "Create database tester${i}"
  psql -c "CREATE DATABASE tester${i} WITH ENCODING 'UTF8' LC_COLLATE='en_US.UTF-8' LC_CTYPE='en_US.UTF-8' TEMPLATE=template0;"
  psql -c "ALTER DATABASE tester${i} OWNER TO tester;"
  psql -d "tester${i}" -c "ALTER SCHEMA public OWNER to tester;"
  echo "tester${i} created!"
done