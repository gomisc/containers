ver:
	@git describe --always

update:
	@go get -u ./...

gomod:
	@go mod tidy -compat=1.17
	@go mod download

test:
	@go test ./...