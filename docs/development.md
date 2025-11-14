# Development

## Setup
```bash
./setup.sh
```

## Commands
```bash
go build ./...              # Build
go test ./...               # Test
gofmt -w .                  # Format
golangci-lint run           # Lint
gosec ./...                 # Security scan
```

## Verification
```bash
go run verification/examples/codemapping_verify.go
go run verification/examples/rag_verify.go
```

## Guidelines
- See CLAUDE.md for coding standards
- Follow SOLID + KISS + YAGNI principles
- Maintain >80% test coverage
- Run verification before committing
