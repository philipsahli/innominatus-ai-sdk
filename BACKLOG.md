# Backlog

## Bugs

### [P1] Fix SearchResult.Score field name mismatch in RAG (ID: BL-BUG-002)
- **Description**: The rag_verify.go script references SearchResult.Score but the actual field name in pkg/platformai/rag/types.go:23 is 'Similarity' not 'Score'. This causes 5 compilation errors. Either: 1) rename Similarity to Score in types.go for consistency, or 2) update all references in rag_verify.go to use Similarity. Option 1 is preferred for better API naming (Score is more intuitive than Similarity).
- **Priority**: P1 (High)
- **Effort**: S (Small, <2h)
- **Source**: Build Errors
- **Added**: 2025-10-20
- **Files**: pkg/platformai/rag/types.go, verification/examples/rag_verify.go
### [P0] Fix verification script compilation errors (ID: BL-BUG-001)
- **Description**: The rag_verify.go and codemapping_verify.go scripts in verification/examples/ have multiple compilation errors: 1) Both define main() causing redeclaration conflict, 2) SearchResult.Score field doesn't exist (should be Similarity), 3) Function redeclarations (saveArtifact, fail, pass), 4) saveArtifact signature mismatch. These errors prevent the build from succeeding and block verification-first development workflow.
- **Priority**: P0 (Critical)
- **Effort**: M (Medium, 2-8h)
- **Source**: Build Errors
- **Added**: 2025-10-20
- **Files**: verification/examples/rag_verify.go, verification/examples/codemapping_verify.go, pkg/platformai/rag/types.go
## Improvements

### [P3] Add verbose logging option to SDK (ID: BL-IMP-003)
- **Description**: No structured logging exists for debugging SDK operations. Add optional verbose mode (controlled by Config.Verbose bool) that logs: LLM request/response sizes, API call durations, repository scan statistics, RAG search results count. Use Go's log/slog package (mentioned in CLAUDE.md monitoring section). Helps troubleshoot issues without enabling debug builds.
- **Priority**: P3 (Low)
- **Effort**: L (Large, 1-3d)
- **Source**: Manual Review
- **Added**: 2025-10-20
- **Files**: pkg/platformai/config.go, pkg/platformai/sdk.go, pkg/platformai/llm/anthropic.go, pkg/platformai/codemapping/analyzer.go, pkg/platformai/rag/retriever.go
### [P2] Add context timeout validation for all LLM calls (ID: BL-IMP-002)
- **Description**: While anthropic.go accepts context.Context, not all LLM calls enforce explicit timeouts. CLAUDE.md specifies default 60s timeout for LLM operations. Add context.WithTimeout wrapper in sdk.go or llm client initialization to ensure all Generate/GenerateWithContext/GenerateWithTools calls respect timeout. Prevents hanging on slow/stalled API requests.
- **Priority**: P2 (Medium)
- **Effort**: M (Medium, 2-8h)
- **Source**: Manual Review
- **Added**: 2025-10-20
- **Files**: pkg/platformai/llm/anthropic.go, pkg/platformai/sdk.go
### [P2] Add benchmarks for RAG vector search performance (ID: BL-IMP-001)
- **Description**: No performance benchmarks exist for RAG vector similarity search in store.go. Add benchmarks testing: search with different document counts (100, 1K, 10K), embedding dimension impact (384 vs 1536), TopK variations (1, 5, 10, 50). Track allocations and execution time. This data informs when to move from in-memory to persistent store per CLAUDE.md guidelines.
- **Priority**: P2 (Medium)
- **Effort**: M (Medium, 2-8h)
- **Source**: Manual Review
- **Added**: 2025-10-20
- **Files**: pkg/platformai/rag/store_test.go, pkg/platformai/rag/store_benchmark_test.go
## Maintenance

### [P2] Extract duplicate utility functions in verification scripts (ID: BL-MNT-011)
- **Description**: Both codemapping_verify.go and rag_verify.go duplicate helper functions: saveArtifact (similar logic), fail (identical stderr + exit), pass (identical formatting). Extract to shared verification/common/helpers.go package with: SaveJSON(), Fail(), Pass(), PrintHeader(). Reduces duplication and ensures consistent verification output formatting across all scripts.
- **Priority**: P2 (Medium)
- **Effort**: S (Small, <2h)
- **Source**: Code Quality
- **Added**: 2025-10-20
- **Files**: verification/common/helpers.go, verification/examples/codemapping_verify.go, verification/examples/rag_verify.go
### [P2] Add integration tests with mocked LLM responses (ID: BL-MNT-010)
- **Description**: Need integration tests that mock Anthropic API responses to test end-to-end flows without real API calls: repository analysis → LLM config generation, RAG retrieval → LLM answer generation, error scenarios (rate limits, invalid responses), tool use workflows. Use httptest to mock API server. Enables testing complete user journeys in CI without API keys.
- **Priority**: P2 (Medium)
- **Effort**: L (Large, 1-3d)
- **Source**: Manual Review
- **Added**: 2025-10-20
- **Files**: pkg/platformai/integration_test.go, pkg/platformai/testdata/mock_responses.json
### [P2] Add godoc comments for all exported functions (ID: BL-MNT-009)
- **Description**: Many exported functions across pkg/ lack documentation comments. Go convention requires doc comments starting with the function name. Missing docs for key functions in: analyzer.go (Analyze, parseGoMod, etc.), detector.go (DetectLanguage, DetectFramework), embeddings.go (GenerateEmbedding), anthropic.go (Generate, GenerateWithContext). Add concise comments explaining purpose, parameters, and return values.
- **Priority**: P2 (Medium)
- **Effort**: M (Medium, 2-8h)
- **Source**: Manual Review
- **Added**: 2025-10-20
- **Files**: pkg/platformai/codemapping/analyzer.go, pkg/platformai/codemapping/detector.go, pkg/platformai/rag/embeddings.go, pkg/platformai/llm/anthropic.go
### [P1] Separate verification scripts into individual binaries (ID: BL-MNT-008)
- **Description**: Both codemapping_verify.go and rag_verify.go are in verification/examples/ causing 'main redeclared' errors and function conflicts (saveArtifact, fail, pass). Move to separate directories: verification/codemapping/ and verification/rag/ with their own main packages. This allows independent building and execution as intended by verification-first development.
- **Priority**: P1 (High)
- **Effort**: M (Medium, 2-8h)
- **Source**: Build Errors
- **Added**: 2025-10-20
- **Files**: verification/codemapping/verify.go, verification/rag/verify.go, verification/examples/codemapping_verify.go, verification/examples/rag_verify.go
### [P1] Add test coverage enforcement to pre-commit hooks (ID: BL-MNT-007)
- **Description**: The hooks.go file doesn't enforce the 80% test coverage requirement. Add a pre-commit check that: runs 'go test -cover ./...' and fails if coverage <80%, shows which packages are below threshold with actionable guidance, allows --no-verify flag for emergencies. This ensures coverage requirements from CLAUDE.md are automatically enforced.
- **Priority**: P1 (High)
- **Effort**: S (Small, <2h)
- **Source**: Manual Review
- **Added**: 2025-10-20
- **Files**: .claude/hooks.go
### [P1] Set up GitHub Actions CI/CD pipeline (ID: BL-MNT-006)
- **Description**: No CI/CD workflows exist (.github/workflows/ directory is missing). Need automated pipeline to: run tests on push/PR, enforce 80% coverage threshold, run golangci-lint and gosec security scan, run verification scripts, build examples. This blocks continuous integration and quality gates as specified in CLAUDE.md infrastructure requirements.
- **Priority**: P1 (High)
- **Effort**: M (Medium, 2-8h)
- **Source**: Manual Review
- **Added**: 2025-10-20
- **Files**: .github/workflows/ci.yml, .github/workflows/verification.yml
### [P1] Simplify README.md to <50 lines (currently 646) (ID: BL-MNT-005)
- **Description**: README.md is 646 lines, violating the minimal documentation philosophy from CLAUDE.md (should be 10-20 lines, 'Telegram style'). Extract detailed API documentation to docs/api.md if needed. Keep only: project description (2-3 sentences), quick start code example, installation command, and link to full docs. Remove verbose feature lists and redundant examples.
- **Priority**: P1 (High)
- **Effort**: S (Small, <2h)
- **Source**: Manual Review
- **Added**: 2025-10-20
- **Files**: README.md, docs/api.md
### [P1] Add unit tests for LLM module (ID: BL-MNT-004)
- **Description**: The llm package (anthropic.go, client.go) has 0% test coverage. Need tests for: API request/response handling, error handling (400/500 responses), tool use functionality, context timeout behavior, response cleaning/parsing, retry logic. Use mock HTTP responses to avoid real API calls in tests.
- **Priority**: P1 (High)
- **Effort**: M (Medium, 2-8h)
- **Source**: Coverage Report
- **Added**: 2025-10-20
- **Files**: pkg/platformai/llm/anthropic_test.go, pkg/platformai/llm/client_test.go
### [P1] Add unit tests for RAG module (ID: BL-MNT-003)
- **Description**: The rag package (embeddings.go, store.go, retriever.go, module.go) has 0% test coverage. Need tests for: embedding generation (mock API calls), vector store operations (add, search, get, delete), similarity calculation accuracy, retriever context formatting, batch operations, error scenarios. Mock external API calls to embedding providers.
- **Priority**: P1 (High)
- **Effort**: L (Large, 1-3d)
- **Source**: Coverage Report
- **Added**: 2025-10-20
- **Files**: pkg/platformai/rag/embeddings_test.go, pkg/platformai/rag/store_test.go, pkg/platformai/rag/retriever_test.go, pkg/platformai/rag/module_test.go
### [P1] Add unit tests for codemapping module (ID: BL-MNT-002)
- **Description**: The codemapping package (analyzer.go, detector.go, config_generator.go, module.go) has 0% test coverage. Need comprehensive tests for: repository analysis with various languages, language/framework detection accuracy, dependency parsing (go.mod, package.json, requirements.txt), config generation validation, error handling paths. Use table-driven tests as per CLAUDE.md guidelines.
- **Priority**: P1 (High)
- **Effort**: L (Large, 1-3d)
- **Source**: Coverage Report
- **Added**: 2025-10-20
- **Files**: pkg/platformai/codemapping/analyzer_test.go, pkg/platformai/codemapping/detector_test.go, pkg/platformai/codemapping/config_generator_test.go, pkg/platformai/codemapping/module_test.go
### [P0] Create test files for all core modules (0% coverage) (ID: BL-MNT-001)
- **Description**: The project has 0% test coverage across all 17 production Go files in pkg/. No _test.go files exist. This violates the >80% coverage requirement stated in CLAUDE.md and blocks quality assurance. Need to create test files for: llm/anthropic.go, codemapping/analyzer.go, codemapping/detector.go, codemapping/config_generator.go, rag/embeddings.go, rag/store.go, rag/retriever.go, sdk.go, config.go.
- **Priority**: P0 (Critical)
- **Effort**: XL (Extra Large, >3d)
- **Source**: Coverage Report
- **Added**: 2025-10-20
- **Files**: pkg/platformai/llm/anthropic_test.go, pkg/platformai/codemapping/analyzer_test.go, pkg/platformai/codemapping/detector_test.go, pkg/platformai/rag/embeddings_test.go, pkg/platformai/rag/store_test.go, pkg/platformai/rag/retriever_test.go, pkg/platformai/sdk_test.go, pkg/platformai/config_test.go
## DX

### [P3] Add progress indicators for long-running operations (ID: BL-DX-005)
- **Description**: Operations like large repository scanning, batch embedding generation, and LLM calls can take 10+ seconds with no feedback. Add progress indicators using simple 'Processing...' dots or percentage for: repository file walking in analyzer.go (show file count), batch embedding in rag module (show X/N documents), LLM generation (show 'Waiting for response...'). Improves perceived performance.
- **Priority**: P3 (Low)
- **Effort**: M (Medium, 2-8h)
- **Source**: Manual Review
- **Added**: 2025-10-20
- **Files**: pkg/platformai/codemapping/analyzer.go, pkg/platformai/rag/retriever.go, pkg/platformai/llm/anthropic.go
### [P3] Improve error messages with actionable guidance (ID: BL-DX-004)
- **Description**: Current error messages are generic ('failed to X: %w'). Enhance with specific guidance: config validation errors should suggest fixes (e.g., 'ANTHROPIC_API_KEY not set. Get one at https://console.anthropic.com'), repository analysis errors should suggest checking paths/permissions, LLM errors should differentiate rate limits vs auth vs network. Makes errors more helpful for developers.
- **Priority**: P3 (Low)
- **Effort**: M (Medium, 2-8h)
- **Source**: Manual Review
- **Added**: 2025-10-20
- **Files**: pkg/platformai/config.go, pkg/platformai/codemapping/analyzer.go, pkg/platformai/llm/anthropic.go, pkg/platformai/errors.go
### [P3] Add shell completions for CLI examples (ID: BL-DX-003)
- **Description**: The code-analyzer example uses Cobra which supports shell completions (bash, zsh, fish, PowerShell), but completion generation is not enabled. Add completion command to rootCmd and document usage in examples/README.md. Improves CLI UX by enabling tab-completion for commands, flags, and file paths. Minimal effort with Cobra's built-in support.
- **Priority**: P3 (Low)
- **Effort**: S (Small, <2h)
- **Source**: Manual Review
- **Added**: 2025-10-20
- **Files**: examples/code-analyzer/main.go
### [P2] Add pre-commit hook configuration template (ID: BL-DX-002)
- **Description**: While hooks.go exists, there's no documented pre-commit hook setup for contributors. Add .git-hooks/pre-commit template that: runs gofmt, golangci-lint, go test with coverage check, prevents commits if checks fail. Include installation instructions in CONTRIBUTING.md. Ensures all contributors follow same quality standards.
- **Priority**: P2 (Medium)
- **Effort**: S (Small, <2h)
- **Source**: Manual Review
- **Added**: 2025-10-20
- **Files**: .git-hooks/pre-commit, CONTRIBUTING.md
### [P2] Create examples README with usage instructions (ID: BL-DX-001)
- **Description**: The examples/ directory (code-analyzer, rag-demo) lacks a README explaining what each example does, how to run them, required environment variables, and expected output. Add examples/README.md with: brief description of each example, setup steps, command-line usage, sample output. Improves DX for new users exploring the SDK.
- **Priority**: P2 (Medium)
- **Effort**: S (Small, <2h)
- **Source**: Manual Review
- **Added**: 2025-10-20
- **Files**: examples/README.md
## High Priority

### Testing (Current Sprint)
- [ ] **Add unit tests** - Core modules (llm, codemapping, rag) - Target: >80% coverage
- [ ] **Integration tests** - LLM/RAG with mocked responses
- [ ] **Test coverage enforcement** - Add to hooks.go (fail if <80%)
- [ ] **Example verification scripts** - Add more to verification/examples/

### Documentation
- [ ] **Simplify README.md** - 646 lines → 10-20 lines (align with minimal philosophy)
- [ ] **Move API docs** - README → docs/api.md (if needed)

## Medium Priority

### Performance
- [ ] **Profile large repos** - Benchmark analysis on >10K files
- [ ] **Optimize file scanning** - Stream instead of load all into memory
- [ ] **RAG benchmarks** - Vector search performance testing

### Code Quality
- [ ] **Security audit** - Run gosec, fix issues
- [ ] **Dependency audit** - Check for unused deps with `go mod tidy && go list -m all`
- [ ] **Error handling review** - Ensure all errors wrapped with context

### Infrastructure
- [ ] **CI/CD setup** - GitHub Actions for tests + verification
- [ ] **Automated verification** - Run verification scripts in CI
- [ ] **Release automation** - Version tagging, changelog generation
- [ ] **Dependency scanning** - Dependabot or similar

## Low Priority

### Features (YAGNI - only if needed)
- [ ] **Persistent vector store** - When in-memory insufficient (>10K docs)
- [ ] **Additional LLM providers** - Google, OpenAI, etc. (when requested)
- [ ] **Additional embedding providers** - Beyond OpenAI/Voyage (when needed)
- [ ] **Streaming analysis** - Real-time progress for large repos

### Nice-to-Have
- [ ] **CLI improvements** - Progress bars, colored output
- [ ] **Configuration file support** - .platformai.yaml for defaults
- [ ] **Caching** - LLM responses for repeated analyses
- [ ] **Telemetry** - Optional usage metrics (opt-in)

## Done

- [x] Core SDK architecture established
- [x] RAG module implemented
- [x] Code analysis working
- [x] Multi-language detection (Go, Node.js, Python, Rust, Java, Ruby, PHP)
- [x] Framework detection (Gin, Express, FastAPI, etc.)
- [x] CLI examples (code-analyzer, rag-demo)
- [x] Verification-first development setup
- [x] Claude Code configuration (CLAUDE.md, DIGEST.md, hooks, agents)
- [x] Setup script with health checks
- [x] Environment variables documented (.env.example)

## Notes

- **Philosophy:** Follow SOLID + KISS + YAGNI
- **Test Coverage:** Minimum 80% required
- **Documentation:** Keep minimal, "Telegram style"
- **Dependencies:** Audit regularly, avoid bloat
- **Performance:** Optimize when needed, not preemptively

---

**Last Updated:** 2025-10-19
