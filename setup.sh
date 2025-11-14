#!/bin/bash

# Platform AI SDK - Development Setup Script
# Validates environment, installs dependencies, and prepares development environment

set -e  # Exit on error

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# Logging functions
log_info() {
    echo -e "${BLUE}ℹ${NC} $1"
}

log_success() {
    echo -e "${GREEN}✓${NC} $1"
}

log_warning() {
    echo -e "${YELLOW}⚠${NC} $1"
}

log_error() {
    echo -e "${RED}✗${NC} $1"
}

# Banner
echo ""
echo "╔════════════════════════════════════════╗"
echo "║   Platform AI SDK - Setup Script      ║"
echo "╔════════════════════════════════════════╗"
echo ""

# 1. Check Prerequisites
log_info "Checking prerequisites..."

# Check Go installation
if ! command -v go &> /dev/null; then
    log_error "Go is not installed"
    echo "  Please install Go 1.24+ from https://go.dev/dl/"
    exit 1
fi

GO_VERSION=$(go version | awk '{print $3}' | sed 's/go//')
log_success "Go $GO_VERSION installed"

# Check minimum Go version (1.24+)
REQUIRED_VERSION="1.24"
if ! printf '%s\n' "$REQUIRED_VERSION" "$GO_VERSION" | sort -V -C; then
    log_warning "Go $GO_VERSION detected, but Go $REQUIRED_VERSION+ recommended"
fi

# Check Git
if ! command -v git &> /dev/null; then
    log_warning "Git not installed (optional but recommended)"
else
    log_success "Git installed"
fi

# 2. Validate Environment Variables
log_info "Validating environment variables..."

# Check ANTHROPIC_API_KEY
if [ -z "$ANTHROPIC_API_KEY" ]; then
    log_warning "ANTHROPIC_API_KEY not set"
    echo "  Set it with: export ANTHROPIC_API_KEY='your-key'"
    echo "  Required for LLM operations"
else
    log_success "ANTHROPIC_API_KEY configured"
fi

# Check optional API keys
if [ -z "$OPENAI_API_KEY" ]; then
    log_warning "OPENAI_API_KEY not set (optional, needed for OpenAI embeddings)"
else
    log_success "OPENAI_API_KEY configured"
fi

if [ -z "$VOYAGEAI_API_KEY" ]; then
    log_warning "VOYAGEAI_API_KEY not set (optional, needed for Voyage AI embeddings)"
else
    log_success "VOYAGEAI_API_KEY configured"
fi

# 3. Install Dependencies
log_info "Installing Go dependencies..."
if go mod download; then
    log_success "Dependencies installed"
else
    log_error "Failed to download dependencies"
    exit 1
fi

# Run go mod tidy to clean up
log_info "Tidying dependencies..."
if go mod tidy; then
    log_success "Dependencies tidied"
else
    log_error "Failed to tidy dependencies"
    exit 1
fi

# 4. Install Development Tools (optional)
log_info "Checking development tools..."

# golangci-lint
if ! command -v golangci-lint &> /dev/null; then
    log_warning "golangci-lint not installed (recommended for linting)"
    echo "  Install with: curl -sSfL https://raw.githubusercontent.com/golangci/golangci-lint/master/install.sh | sh -s -- -b \$(go env GOPATH)/bin"
else
    log_success "golangci-lint installed"
fi

# gosec
if ! command -v gosec &> /dev/null; then
    log_warning "gosec not installed (recommended for security scanning)"
    echo "  Install with: go install github.com/securego/gosec/v2/cmd/gosec@latest"
else
    log_success "gosec installed"
fi

# 5. Build Project
log_info "Building project..."
if go build ./...; then
    log_success "Project built successfully"
else
    log_error "Build failed"
    exit 1
fi

# 6. Build Examples
log_info "Building examples..."

# Code analyzer
if cd examples/code-analyzer && go build -o code-analyzer && cd ../..; then
    log_success "code-analyzer built"
else
    log_error "Failed to build code-analyzer"
fi

# RAG demo
if cd examples/rag-demo && go build -o rag-demo && cd ../..; then
    log_success "rag-demo built"
else
    log_error "Failed to build rag-demo"
fi

# 7. Run Tests (if any exist)
log_info "Running tests..."
if go test ./... 2>&1 | grep -q "no test files"; then
    log_warning "No tests found (tests needed: see verification/ for examples)"
else
    if go test ./...; then
        log_success "All tests passed"
    else
        log_error "Some tests failed"
    fi
fi

# 8. Create Required Directories
log_info "Creating directories..."
mkdir -p docs/verification
log_success "Created docs/verification/"

# 9. Health Checks
log_info "Running health checks..."

# Check if test data exists
if [ -d "testdata/sample-go-repo" ]; then
    log_success "Test data available"
else
    log_warning "No test data found in testdata/"
fi

# Check if hooks are executable
if [ -f ".claude/hooks.go" ]; then
    log_success "Claude hooks configured"
else
    log_warning "No Claude hooks found"
fi

# 10. Display Success Dashboard
echo ""
echo "╔════════════════════════════════════════╗"
echo "║   Setup Complete! 🎉                  ║"
echo "╚════════════════════════════════════════╝"
echo ""

echo "📦 Installation Summary:"
echo "  ✓ Go $GO_VERSION"
echo "  ✓ Dependencies installed"
echo "  ✓ Project built"
echo "  ✓ Examples compiled"
echo ""

echo "🚀 Next Steps:"
echo ""
echo "  1. Set API keys (if not already set):"
echo "     export ANTHROPIC_API_KEY='your-key'"
echo "     export OPENAI_API_KEY='your-key'  # optional"
echo ""
echo "  2. Run code analysis example:"
echo "     ./examples/code-analyzer/code-analyzer analyze testdata/sample-go-repo"
echo ""
echo "  3. Run RAG demo (requires OPENAI_API_KEY):"
echo "     ./examples/rag-demo/rag-demo"
echo ""
echo "  4. Run verification scripts:"
echo "     go run verification/examples/codemapping_verify.go"
echo "     go run verification/examples/rag_verify.go"
echo ""
echo "  5. Start development with Claude Code:"
echo "     claude"
echo ""

echo "📚 Resources:"
echo "  • CLAUDE.md - Development guidelines"
echo "  • DIGEST.md - Architecture overview"
echo "  • README.md - Full documentation"
echo "  • verification/ - Verification scripts and templates"
echo ""

echo "🔧 Development Commands:"
echo "  go build ./...              # Build all packages"
echo "  go test ./...               # Run tests"
echo "  gofmt -w .                  # Format code"
echo "  golangci-lint run           # Lint code"
echo "  gosec ./...                 # Security scan"
echo ""

# Check for missing API keys and display reminder
if [ -z "$ANTHROPIC_API_KEY" ]; then
    echo "⚠️  Remember to set ANTHROPIC_API_KEY before using SDK features"
    echo ""
fi

log_success "Setup completed successfully!"
echo ""
