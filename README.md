# Codebase Upload System - Separated Architecture

This system is now split into two separate servers:

## Server A (API/Frontend Server)
- **Port**: 8080
- **Purpose**: Handles the web frontend and API requests
- **Database**: PostgreSQL for metadata storage
- **Location**: `./server-a/`

### Features:
- Serves the HTML/CSS/JS frontend
- Handles file upload requests
- Stores metadata in PostgreSQL database
- Forwards files to Server B for storage
- Proxies file download/content requests to Server B

### Database Schema:
- `codebases` table: stores codebase metadata (ID, creation time, file count)
- `files` table: stores file metadata (path, name, size, codebase reference)

## Server B (Storage Server)
- **Port**: 8081
- **Purpose**: File storage and retrieval
- **Storage**: Local filesystem in `./storage/` directory
- **Location**: `./server-b/`

### Features:
- Stores uploaded files in organized directory structure
- Serves file content and metadata
- Handles file downloads
- Creates and serves ZIP archives of codebases

## Running the System

### Option 1: Docker Compose (Recommended)
```bash
docker-compose up --build
```

This will start:
- PostgreSQL database on port 5432
- Server A on port 8080
- Server B on port 8081

### Option 2: Manual Setup

1. **Start PostgreSQL**:
   ```bash
   # Install and start PostgreSQL
   createdb codebase_db
   ```

2. **Start Server B (Storage)**:
   ```bash
   cd server-b
   go mod tidy
   go run main.go
   ```

3. **Start Server A (API)**:
   ```bash
   cd server-a
   go mod tidy
   export DATABASE_URL="postgres://user:password@localhost/codebase_db?sslmode=disable"
   go run main.go
   ```

4. **Access the application**:
   Open http://localhost:8080 in your browser

## Environment Variables

### Server A:
- `DATABASE_URL`: PostgreSQL connection string
- `PORT`: Server port (default: 8080)
- `STORAGE_SERVER_URL`: URL of Server B (default: http://localhost:8081)

### Server B:
- `PORT`: Server port (default: 8081)
- `STORAGE_DIR`: Directory for file storage (default: ./storage)

## API Communication

Server A communicates with Server B through HTTP requests:
- File uploads: `POST /store`
- File content: `GET /content/{id}?file=path`
- File downloads: `GET /download/{id}?file=path`
- ZIP downloads: `GET /zip/{id}`

## Architecture Benefits

1. **Separation of Concerns**: API logic separated from file storage
2. **Scalability**: Each server can be scaled independently
3. **Maintainability**: Simpler, focused codebases
4. **Flexibility**: Storage server can be replaced or enhanced without affecting API
5. **Security**: Database credentials only needed in Server A