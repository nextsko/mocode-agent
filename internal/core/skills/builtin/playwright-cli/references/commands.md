# Playwright CLI — Command Reference

Complete reference for Playwright browser automation commands.

## Navigation

```bash
# Open a page
npx playwright open https://example.com

# Generate a test from recording
npx playwright codegen https://example.com

# Screenshot
npx playwright screenshot https://example.com screenshot.png
npx playwright screenshot --full-page https://example.com full.png

# PDF
npx playwright pdf https://example.com page.pdf
```

## Running Tests

```bash
# Run all tests
npx playwright test

# Run specific test file
npx playwright test tests/login.spec.ts

# Run with UI mode
npx playwright test --ui

# Run headed (visible browser)
npx playwright test --headed

# Run specific project
npx playwright test --project=chromium

# Debug mode
npx playwright test --debug

# Last failed tests only
npx playwright test --last-failed

# Update snapshots
npx playwright test --update-snapshots
```

## Test Generation

```bash
# Generate test from recording
npx playwright codegen

# Generate with specific browser
npx playwright codegen --browser=firefox

# Generate and save to file
npx playwright codegen -o tests/generated.spec.ts

# Generate targeting mobile viewport
npx playwright codegen --viewport-size=390,844
```

## Report & Trace

```bash
# Show HTML report
npx playwright show-report

# Show trace viewer
npx playwright show-trace trace.zip

# Merge reports
npx playwright merge-reports --reporter=html ./report1 ./report2
```

## Project & Config

```bash
# Initialize Playwright
npm init playwright@latest

# Install browsers
npx playwright install
npx playwright install chromium
npx playwright install --with-deps

# Install system dependencies
npx playwright install-deps

# List projects
npx playwright list
```
