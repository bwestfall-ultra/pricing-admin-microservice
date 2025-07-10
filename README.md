# Enhanced Pricing Admin

## Running the Microservice

create .env file from .sample_env

go run main.go


## Running as a Service

**RUN** docker network create pricing-net

create .env file from .sample_env

pricing-microservice/ \
**RUN** docker-compose up -d


// Can be run from Redis CLI to remove all records for a given pattern
EVAL "for _,k in ipairs(redis.call('keys','price:wholesale:*')) do redis.call('del',k) end" 0

