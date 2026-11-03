# Contributing to Gobelisk

First off, thank you for considering contributing to Gobelisk! It's people like you that make Gobelisk such a great tool.

## Code of Conduct

This project and everyone participating in it is governed by our [Code of Conduct](CODE_OF_CONDUCT.md). By participating, you are expected to uphold this code.

## How Can I Contribute?

### Reporting Bugs

Before creating bug reports, please check the existing issues as you might find out that you don't need to create one. When you are creating a bug report, please include as many details as possible using our [bug report template](.github/ISSUE_TEMPLATE/bug_report.md).

### Suggesting Enhancements

Enhancement suggestions are tracked as GitHub issues. When creating an enhancement suggestion, please use our [feature request template](.github/ISSUE_TEMPLATE/feature_request.md) and provide as much detail as possible.

### Pull Requests

1. Fork the repo and create your branch from `master`.
2. If you've added code that should be tested, add tests.
3. If you've changed APIs, update the documentation.
4. Ensure the test suite passes (`go test ./...`).
5. Make sure your code lints (`go vet ./...`).
6. Issue that pull request!

## Development Setup

### Prerequisites

- Go 1.26 or higher
- Git
- (Optional) buf CLI for protobuf generation

### Getting Started

```bash
# Clone your fork
git clone https://github.com/YOUR_USERNAME/gobelisk.git
cd gobelisk

# Add upstream remote
git remote add upstream https://github.com/pilab-cloud/gobelisk.git

# Install dependencies
go mod download

# Build
go build ./...

# Run tests
go test ./...
```

### Code Style

- Follow standard Go conventions (gofmt, go vet)
- Write clear, descriptive commit messages
- Add comments for exported functions and types
- Keep functions small and focused

### Commit Messages

We follow conventional commit style:

- `feat: add new feature`
- `fix: resolve bug`
- `docs: update documentation`
- `refactor: code refactoring`
- `test: add tests`
- `chore: maintenance tasks`

## Project Structure

See our [README](README.md) for an overview of the project structure.

## Questions?

Feel free to open a [question issue](.github/ISSUE_TEMPLATE/question.md) or reach out on [Discord](https://discord.gg/).

## License

By contributing, you agree that your contributions will be licensed under the MIT License.
