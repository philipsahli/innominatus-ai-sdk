# Platform AI Example - Code Analyzer CLI

A command-line tool that demonstrates the Platform AI SDK by analyzing code repositories and generating platform configurations.

## Installation

```bash
# Clone the repository
git clone https://github.com/philipsahli/innominatus-ai-sdk
cd innominatus-ai-sdk/examples/code-analyzer

# Build the tool
go build -o platform-ai-example

# Or install it
go install
```

## Usage

### Basic Analysis

```bash
export ANTHROPIC_API_KEY="your-api-key"
./platform-ai-example analyze /path/to/your/repository
```

### Options

```
Usage:
  platform-ai-example analyze [repository-path] [flags]

Flags:
  -o, --output string   Output path for config file (default: .platform/config.yaml)
  -v, --verbose         Verbose output
  -f, --format string   Output format (yaml) (default "yaml")
  -h, --help            Help for analyze
```

### Examples

```bash
# Analyze repository and save config to default location
./platform-ai-example analyze /path/to/repo

# Specify custom output path
./platform-ai-example analyze /path/to/repo -o custom-config.yaml

# Enable verbose output
./platform-ai-example analyze /path/to/repo -v

# Show version
./platform-ai-example --version
```

## Sample Repositories

Try the tool with the included sample repositories:

```bash
# Go repository (Gin framework)
./platform-ai-example analyze ../../testdata/sample-go-repo

# Node.js repository (Express framework)
./platform-ai-example analyze ../../testdata/sample-node-repo

# Python repository (FastAPI framework)
./platform-ai-example analyze ../../testdata/sample-python-repo
```

## Output

The tool generates:

1. **Console Report** - Detailed analysis with color-coded sections
2. **YAML Configuration** - Platform config file at `.platform/config.yaml`

### Console Output Sections

- **Stack Detection** - Language, framework, and file statistics
- **Detected Dependencies** - Key dependencies and versions
- **Platform Services** - Service configuration and required services
- **Resource Recommendations** - CPU, memory, and scaling parameters
- **Monitoring** - Monitoring configuration
- **Recommendations** - Actionable suggestions for improvements

## Requirements

- Go 1.21 or later
- Valid Anthropic API key
- Internet connection for API calls

## Troubleshooting

### Missing API Key

```
Error: ANTHROPIC_API_KEY environment variable is required
```

**Solution:** Set your Anthropic API key:
```bash
export ANTHROPIC_API_KEY="your-api-key"
```

### Repository Not Found

```
Error: repository path does not exist: /path/to/repo
```

**Solution:** Verify the path exists and is accessible

### API Errors

If you encounter API errors, check:
- Your API key is valid
- You have sufficient API credits
- Your internet connection is working
- You're not hitting rate limits

## License

MIT License - see [LICENSE](../../LICENSE) for details