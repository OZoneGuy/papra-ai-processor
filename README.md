# Papra AI Processor

A Go microservice that processes documents from [Papra](https://papra.app) using AI. When documents are uploaded or tagged in Papra, this service extracts metadata (content, name, tags, dates) using OpenRouter's AI models and updates the documents automatically.

## Prerequisites

- **Go** 1.26.3 or later
- **Docker** for building container images
- **Git** for version control

## Environment Variables

The service requires the following environment variables:

| Variable | Description |
|----------|-------------|
| `OPENROUTER_API_KEY` | API key for OpenRouter (AI service) |
| `PAPRA_API_KEY` | API key for your Papra instance |
| `PAPRA_DOMAIN` | Domain URL of your Papra instance (e.g., `https://app.papra.app`) |

## Building the Docker Image

To build the Docker image:

```bash
docker build -t <ocr-host>/papra-ai-processor .
```

This builds a minimal static image using `distroless/static-debian13`.

## Running the Container

```bash
docker run -d \
  -e OPENROUTER_API_KEY=your_openrouter_key \
  -e PAPRA_API_KEY=your_papra_key \
  -e PAPRA_DOMAIN=https://app.papra.app \
  -p 3000:3000 \
  papra-ai-processor
```

## Development

### Local Build

```bash
go build -o app .
./app
```

### Run with Go

```bash
go run .
```

### Dependencies

The project uses Go modules. Dependencies are defined in `go.mod`.

```
github.com/OpenRouterTeam/go-sdk      - OpenRouter AI client
github.com/gofiber/fiber/v3           - HTTP web framework
```

## API Endpoint

### POST /process-document

Receives webhook events from Papra and processes documents.

**Triggers:**
- `document:created` - New document uploaded
- `document:tag:added` - Document tagged with "To-Process"

**Processing:**
1. Downloads the document from Papra
2. Sends the document to OpenRouter's AI (Gemini 3.1 flash lite)
3. Extracts: content, name, tags, document date, expiry date
4. Updates the document in Papra with extracted metadata
5. Adds appropriate tags and removes the "To-Process" tag

## Project Structure

```
.
├── main.go          # Main application logic
├── go.mod           # Go module definition
├── go.sum           # Dependency checksums
├── Dockerfile       # Docker image definition
└── .dockerignore    # Docker build exclusions
```
