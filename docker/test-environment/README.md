# SmartRing test environment

This Compose project runs Redis, two MySQL databases, a single-node MongoDB
replica set, and the four application images published by Arcane Quick Build.

1. Copy `.env.example` to `.env` and set the ECR host and four commit tags.
2. Create the JWT private key and matching public key directory referenced by
   `.env`. The public key filename must match `cn-2026-01.pem`:

   ```bash
   mkdir -p /home/arcane/test-environment/secrets/jwt-public-keys
   openssl ecparam -name prime256v1 -genkey -noout \
     -out /home/arcane/test-environment/secrets/jwt-private.pem
   openssl ec -pubout \
     -in /home/arcane/test-environment/secrets/jwt-private.pem \
     -out /home/arcane/test-environment/secrets/jwt-public-keys/cn-2026-01.pem
   chmod 600 /home/arcane/test-environment/secrets/jwt-private.pem
   ```

3. Authenticate Docker to ECR.

   ```bash
   aws ecr get-login-password --region us-west-2 \
     | docker login --username AWS --password-stdin ECR_REGISTRY_HOST
   ```

4. Start the project from the Arcane repository root:

   ```bash
   docker compose \
     --env-file docker/test-environment/.env \
     -f docker/test-environment/compose.yaml \
     up -d --wait
   ```

Middleware ports bind to `127.0.0.1` by default. Set
`MIDDLEWARE_BIND_IP=0.0.0.0` only when remote access is required and protected
by a firewall or cloud security group.
