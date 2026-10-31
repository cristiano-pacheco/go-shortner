#!/bin/bash

# ==============================================================================
# Integration Test Script
# Runs integration tests using the Docker CLI configuration.
#
# Usage:
#   ./scripts/integration-test.sh [command] [options]
#
# Commands:
#   test                    Run all integration tests
#   test-race              Run integration tests with race detector
#   test-cover             Run integration tests with coverage report
#   help                   Show this help message
#
# Environment Variables (optional overrides):
#   DOCKER_HOST                     Override Docker socket location
#
# ==============================================================================

set -e

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# ==============================================================================
# Helper Functions
# ==============================================================================

print_header() {
    echo -e "${BLUE}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${NC}"
    echo -e "${BLUE}$1${NC}"
    echo -e "${BLUE}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${NC}"
}

print_success() {
    echo -e "${GREEN}✓ $1${NC}"
}

print_error() {
    echo -e "${RED}✗ $1${NC}"
}

print_info() {
    echo -e "${YELLOW}ℹ $1${NC}"
}

# ==============================================================================
# Docker Configuration
# ==============================================================================

setup_docker_environment() {
    print_info "Using Docker CLI configuration"
    docker info >/dev/null
    echo ""
}

# ==============================================================================
# Test Commands
# ==============================================================================

run_integration_tests() {
    print_header "Running Integration Tests"
    setup_docker_environment "$1"

    print_info "Running tests in: ./test/integration/..."
    print_info "Note: Tests run sequentially to prevent container cleanup conflicts"
    echo ""

    # Run tests with p=1 to prevent parallel package execution
    # This ensures one package's cleanup doesn't affect another's containers
    go test -count=1 -v -tags=integration -timeout=10m -p=1 ./test/integration/...
}

run_race_tests() {
    print_header "Running Integration Tests with Race Detector"
    setup_docker_environment "$1"

    print_info "Running tests with -race flag (slower but more thorough)"
    print_info "Note: Tests run sequentially to prevent container cleanup conflicts"
    echo ""

    # Run tests with p=1 to prevent parallel package execution
    go test -count=1 -v -tags=integration -race -timeout=15m -p=1 ./test/integration/...
}

run_coverage_tests() {
    print_header "Running Integration Tests with Coverage"
    setup_docker_environment "$1"

    # Create reports directory
    mkdir -p reports

    print_info "Running tests with coverage"
    print_info "Coverage report will be saved to: reports/integration-cover.html"
    print_info "Note: Tests run sequentially to prevent container cleanup conflicts"
    echo ""

    # Run tests with p=1 to prevent parallel package execution
    go test -count=1 -v -tags=integration -coverpkg=./... -coverprofile=reports/integration-cover.out -timeout=10m -p=1 ./test/integration/...
    go tool cover -html=reports/integration-cover.out -o reports/integration-cover.html
    print_success "Coverage report generated: reports/integration-cover.html"
    print_info "Open the report in your browser to view detailed coverage information"
}

show_help() {
    cat << EOF
$(print_header "Integration Test Script")

Runs integration tests using the Docker CLI configuration.

Usage:
    $(basename "$0") [command]

Commands:
    test                    Run all integration tests
    test-race              Run integration tests with race detector
    test-cover             Run integration tests with coverage report
    help                   Show this help message

Environment Variables (optional overrides):
    DOCKER_HOST                     Override Docker socket location
                                    Example: unix:///var/run/docker.sock

Examples:
    # Run all integration tests
    $0 test

    # Run tests with race detector
    $0 test-race

    # Run tests with coverage
    $0 test-cover

    # Override Docker socket location
    DOCKER_HOST=unix:///custom/path/docker.sock $0 test

EOF
}

# ==============================================================================
# Main Script
# ==============================================================================

main() {
    local command="${1:-help}"

    case "$command" in
        test)
            run_integration_tests "$@"
            ;;
        test-race)
            run_race_tests "$@"
            ;;
        test-cover)
            run_coverage_tests "$@"
            ;;
        help|--help|-h)
            show_help
            ;;
        *)
            print_error "Unknown command: $command"
            echo ""
            show_help
            exit 1
            ;;
    esac
}

# Run main function
main "$@"
