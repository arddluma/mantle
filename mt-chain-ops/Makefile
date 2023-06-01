mt-migrate:
	go build -o ./bin/mt-migrate ./cmd/mt-migrate/main.go

eof-crawler:
	go build -o ./bin/eof-crawler ./cmd/eof-crawler/main.go

check-migration:
	go build -o ./bin/check-migration ./cmd/check-migration/main.go

check-migration-quick:
	go build -o ./bin/check-migration-quick ./cmd/check-migration-quick/main.go

inject-mints:
	go build -o ./bin/inject-mints ./cmd/inject-mints/main.go

rollover:
	go build -o ./bin/rollover ./cmd/rollover/main.go

withdrawals:
	go build -o ./bin/withdrawals ./cmd/withdrawals/main.go

test:
	go test ./...

.PHONY: mt-migrate eof-crawler check-migration check-migration-quick inject-mints rollover withdrawals test
