.PHONY: openapi redoc

openapi:
	go generate ./cmd/api

redoc: openapi
	node scripts/build-redoc.mjs
