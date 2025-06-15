package main

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/mux"
	_ "github.com/lib/pq"
)

const (
	MaxUploadSize = 100 << 20 // 100MB
)

type Server struct {
	db               *sql.DB
	storageServerURL string
}

type UploadResponse struct {
	Success       bool     `json:"success"`
	Message       string   `json:"message"`
	DirectoryID   string   `json:"directory_id,omitempty"`
	UploadedFiles []string `json:"uploaded_files,omitempty"`
}

type FileInfo struct {
	Name string `json:"name"`
	Size int64  `json:"size"`
	Path string `json:"path"`
}

type Codebase struct {
	ID        string    `json:"directory_id"`
	CreatedAt time.Time `json:"created_at"`
	FileCount int       `json:"file_count"`
}

func NewServer() *Server {
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		dbURL = "user=postgres password=password dbname=postgres sslmode=disable"
	}

	storageServerURL := os.Getenv("SERVER_B_URL")
	if storageServerURL == "" {
		storageServerURL = "http://localhost:8081"
	}

	db, err := sql.Open("postgres", dbURL)
	if err != nil {
		log.Fatalf("Failed to connect to database: %v", err)
	}

	if err := db.Ping(); err != nil {
		log.Fatalf("Failed to ping database: %v", err)
	}

	server := &Server{
		db:               db,
		storageServerURL: storageServerURL,
	}
	server.initDB()
	return server
}

func (s *Server) initDB() {
	query := `
	CREATE TABLE IF NOT EXISTS codebases (
		id UUID PRIMARY KEY,
		created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
		file_count INTEGER DEFAULT 0
	);

	CREATE TABLE IF NOT EXISTS files (
		id SERIAL PRIMARY KEY,
		codebase_id UUID REFERENCES codebases(id) ON DELETE CASCADE,
		file_path TEXT NOT NULL,
		file_name TEXT NOT NULL,
		file_size BIGINT NOT NULL,
		created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
	);

	CREATE INDEX IF NOT EXISTS idx_files_codebase_id ON files(codebase_id);
	`

	if _, err := s.db.Exec(query); err != nil {
		log.Fatalf("Failed to initialize database: %v", err)
	}
}

func enableCORS(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "POST, GET, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")

		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusOK)
			return
		}

		next.ServeHTTP(w, r)
	})
}

func (s *Server) uploadCodebase(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, MaxUploadSize)

	if err := r.ParseMultipartForm(MaxUploadSize); err != nil {
		respondWithError(w, http.StatusBadRequest, "File too large or invalid form data")
		return
	}

	files := r.MultipartForm.File["files"]
	if len(files) == 0 {
		respondWithError(w, http.StatusBadRequest, "No files uploaded")
		return
	}

	// Generate UUID for the new codebase
	codebaseID := uuid.New().String()

	// Forward files to storage server
	uploadedFiles, err := s.forwardFilesToStorage(codebaseID, files, r)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, fmt.Sprintf("Failed to store files: %v", err))
		return
	}

	// Store metadata in database
	tx, err := s.db.Begin()
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Database transaction failed")
		return
	}
	defer tx.Rollback()

	// Insert codebase record
	_, err = tx.Exec("INSERT INTO codebases (id, file_count) VALUES ($1, $2)",
		codebaseID, len(uploadedFiles))
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Failed to save codebase metadata")
		return
	}

	// Insert file records
	for _, fileInfo := range uploadedFiles {
		_, err = tx.Exec(`INSERT INTO files (codebase_id, file_path, file_name, file_size) 
			VALUES ($1, $2, $3, $4)`,
			codebaseID, fileInfo.Path, fileInfo.Name, fileInfo.Size)
		if err != nil {
			respondWithError(w, http.StatusInternalServerError, "Failed to save file metadata")
			return
		}
	}

	if err = tx.Commit(); err != nil {
		respondWithError(w, http.StatusInternalServerError, "Failed to commit transaction")
		return
	}

	var filePaths []string
	var totalSize int64
	for _, f := range uploadedFiles {
		filePaths = append(filePaths, f.Path)
		totalSize += f.Size
	}

	response := UploadResponse{
		Success:       true,
		Message:       fmt.Sprintf("Successfully uploaded %d files (%d bytes total)", len(uploadedFiles), totalSize),
		DirectoryID:   codebaseID,
		UploadedFiles: filePaths,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

func (s *Server) forwardFilesToStorage(codebaseID string, files []*multipart.FileHeader, r *http.Request) ([]FileInfo, error) {
	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)

	// Add codebase ID
	writer.WriteField("codebase_id", codebaseID)

	var fileInfos []FileInfo

	for _, fileHeader := range files {
		file, err := fileHeader.Open()
		if err != nil {
			continue
		}
		defer file.Close()

		// Get relative path from form data
		relativePath := r.FormValue("path_" + fileHeader.Filename)
		if relativePath == "" {
			relativePath = fileHeader.Filename
		}

		// Create form file
		part, err := writer.CreateFormFile("files", fileHeader.Filename)
		if err != nil {
			continue
		}

		written, err := io.Copy(part, file)
		if err != nil {
			continue
		}

		// Add path information
		writer.WriteField("path_"+fileHeader.Filename, relativePath)

		fileInfos = append(fileInfos, FileInfo{
			Name: filepath.Base(relativePath),
			Path: relativePath,
			Size: written,
		})
	}

	writer.Close()

	// Send to storage server
	resp, err := http.Post(s.storageServerURL+"/store", writer.FormDataContentType(), &buf)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("storage server returned status %d", resp.StatusCode)
	}

	return fileInfos, nil
}

func (s *Server) listCodebases(w http.ResponseWriter, r *http.Request) {
	rows, err := s.db.Query("SELECT id, created_at, file_count FROM codebases ORDER BY created_at DESC")
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Failed to query codebases")
		return
	}
	defer rows.Close()

	var codebases []Codebase
	for rows.Next() {
		var cb Codebase
		if err := rows.Scan(&cb.ID, &cb.CreatedAt, &cb.FileCount); err != nil {
			continue
		}
		codebases = append(codebases, cb)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success":   true,
		"codebases": codebases,
	})
}

func (s *Server) getCodebaseFiles(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	codebaseID := vars["id"]

	if _, err := uuid.Parse(codebaseID); err != nil {
		respondWithError(w, http.StatusBadRequest, "Invalid directory ID")
		return
	}

	// Check if codebase exists
	var exists bool
	err := s.db.QueryRow("SELECT EXISTS(SELECT 1 FROM codebases WHERE id = $1)", codebaseID).Scan(&exists)
	if err != nil || !exists {
		respondWithError(w, http.StatusNotFound, "Codebase not found")
		return
	}

	// Get files from database
	rows, err := s.db.Query("SELECT file_path, file_name, file_size FROM files WHERE codebase_id = $1", codebaseID)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Failed to query files")
		return
	}
	defer rows.Close()

	var files []FileInfo
	for rows.Next() {
		var f FileInfo
		if err := rows.Scan(&f.Path, &f.Name, &f.Size); err != nil {
			continue
		}
		files = append(files, f)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success":      true,
		"directory_id": codebaseID,
		"files":        files,
	})
}

func (s *Server) readFileContent(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	codebaseID := vars["id"]
	filePath := r.URL.Query().Get("file")

	if _, err := uuid.Parse(codebaseID); err != nil {
		respondWithError(w, http.StatusBadRequest, "Invalid directory ID")
		return
	}

	if filePath == "" {
		respondWithError(w, http.StatusBadRequest, "File path is required")
		return
	}

	// Forward request to storage server
	//log.Printf("Fetching file content for codebase %s, file %s", codebaseID, filePath)
	url := fmt.Sprintf("%s/content/%s?file=%s", s.storageServerURL, codebaseID, filePath)
	resp, err := http.Get(url)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Failed to retrieve file from storage")
		return
	}
	defer resp.Body.Close()

	// Copy response
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(resp.StatusCode)
	io.Copy(w, resp.Body)
}

func (s *Server) downloadFile(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	codebaseID := vars["id"]
	filePath := r.URL.Query().Get("file")

	if _, err := uuid.Parse(codebaseID); err != nil {
		respondWithError(w, http.StatusBadRequest, "Invalid directory ID")
		return
	}

	if filePath == "" {
		respondWithError(w, http.StatusBadRequest, "File path is required")
		return
	}

	// Forward request to storage server
	url := fmt.Sprintf("%s/download/%s?file=%s", s.storageServerURL, codebaseID, filePath)
	//log.Printf("%s/download/%s?file=%s", s.storageServerURL, codebaseID, filePath)
	resp, err := http.Get(url)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Failed to retrieve file from storage")
		return
	}
	defer resp.Body.Close()

	// Copy headers and response
	for key, values := range resp.Header {
		for _, value := range values {
			w.Header().Add(key, value)
		}
	}
	w.WriteHeader(resp.StatusCode)
	io.Copy(w, resp.Body)
}

func (s *Server) downloadZip(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	codebaseID := vars["id"]

	if _, err := uuid.Parse(codebaseID); err != nil {
		respondWithError(w, http.StatusBadRequest, "Invalid directory ID")
		return
	}

	// Forward request to storage server
	url := fmt.Sprintf("%s/zip/%s", s.storageServerURL, codebaseID)
	resp, err := http.Get(url)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Failed to retrieve ZIP from storage")
		return
	}
	defer resp.Body.Close()

	// Copy headers and response
	for key, values := range resp.Header {
		for _, value := range values {
			w.Header().Add(key, value)
		}
	}
	w.WriteHeader(resp.StatusCode)
	io.Copy(w, resp.Body)
}

func (s *Server) healthCheck(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

func respondWithError(w http.ResponseWriter, code int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": false,
		"error":   message,
	})
}

func main() {
	server := NewServer()
	defer server.db.Close()

	r := mux.NewRouter()
	r.Use(enableCORS)

	// API routes
	r.HandleFunc("/upload", server.uploadCodebase).Methods("POST", "OPTIONS")
	r.HandleFunc("/codebases", server.listCodebases).Methods("GET")
	r.HandleFunc("/codebases/{id}", server.getCodebaseFiles).Methods("GET")
	r.HandleFunc("/codebases/{id}/content", server.readFileContent).Methods("GET")
	r.HandleFunc("/codebases/{id}/download", server.downloadFile).Methods("GET")
	r.HandleFunc("/codebases/{id}/zip", server.downloadZip).Methods("GET")
	r.HandleFunc("/health", server.healthCheck).Methods("GET")

	// Serve static files
	r.PathPrefix("/").Handler(http.FileServer(http.Dir("./static/")))

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	log.Printf("Server A starting on port %s", port)
	log.Printf("Storage server URL: %s", server.storageServerURL)
	log.Fatal(http.ListenAndServe(":"+port, r))
}
