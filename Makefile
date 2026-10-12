.PHONY: generate

generate:
	cd server && go tool github.com/oapi-codegen/oapi-codegen/v2/cmd/oapi-codegen --config api/oapi-codegen.yaml api/openapi.yaml
	cd client && pnpm dlx openapi-typescript@7.10.1 ../server/api/openapi.yaml -o lib/api-schema.ts
