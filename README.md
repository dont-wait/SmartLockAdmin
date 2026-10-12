# SmartLockAdmin

Admin app ported from SmartLock: Next.js client, Go HTTP/MQTT server, and one OpenAPI contract used to generate Go and TypeScript types.

## Run

1. Start the MQTT broker used by the ESP32: `cd server && docker compose up -d`.
2. Copy `server/.env.example` to `server/.env`, then load it and start the API from `server`: `set -a; . ./.env; set +a; go run ./cmd/api`.
3. Start the admin UI: `cd client && pnpm install && pnpm dev` (port 3001).

The default API URL is `http://localhost:3000`; the default client URL is `http://localhost:3001`.
Configure SMTP in `server/.env` when email OTP delivery is needed. Set `NEXT_PUBLIC_API_URL` in the client environment when the API runs outside `http://localhost:3000`.

If the old project has `server/data/cards.json`, copy it to `SmartLockAdmin/server/data/cards.json` before starting the Go API. Both the old string-only UID entries and `{ "uid", "email" }` card entries are supported.

## Contract

`server/api/openapi.yaml` is the source contract. `make generate` generates Go DTO and TypeScript API types. The card DTO retains the existing `uid` and `email` JSON fields.

The Go API preserves the current card, camera-frame, and HTTP OTP endpoints, and listens to the existing SmartLock MQTT topics so the current ESP32 firmware remains compatible. Card changes are broadcast to admin clients through WebSocket.
