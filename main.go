package main

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/google/uuid"
	"github.com/gorilla/mux"
)

const (
	// Maximum upload size (100MB)
	MaxUploadSize = 100 << 20
	// Base directory for storing uploaded codebases
	BaseUploadDir = "./uploads"
)

type UploadResponse struct {
	Success     bool     `json:"success"`
	Message     string   `json:"message"`
	DirectoryID string   `json:"directory_id,omitempty"`
	UploadedFiles []string `json:"uploaded_files,omitempty"`
}

type FileInfo struct {
	Name string `json:"name"`
	Size int64  `json:"size"`
	Path string `json:"path"`
}

func init() {
	// Create base upload directory if it doesn't exist
	if err := os.MkdirAll(BaseUploadDir, 0755); err != nil {
		log.Fatalf("Failed to create base upload directory: %v", err)
	}
}

// CORS middleware
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

// uploadCodebase handles the file upload request
func uploadCodebase(w http.ResponseWriter, r *http.Request) {
	// Set maximum request size
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

	// Generate UUID for the new directory
	dirID := uuid.New().String()
	uploadDir := filepath.Join(BaseUploadDir, dirID)

	// Create the directory
	if err := os.MkdirAll(uploadDir, 0755); err != nil {
		respondWithError(w, http.StatusInternalServerError, "Failed to create upload directory")
		return
	}

	var uploadedFiles []string
	var totalSize int64

	// Process each file
	for _, fileHeader := range files {
		file, err := fileHeader.Open()
		if err != nil {
			log.Printf("Error opening file %s: %v", fileHeader.Filename, err)
			continue
		}
		defer file.Close()

		// Validate file name and prevent directory traversal
		fileName := filepath.Base(fileHeader.Filename)
		if fileName == "" || fileName == "." || fileName == ".." {
			log.Printf("Invalid filename: %s", fileHeader.Filename)
			continue
		}

		// Get relative path from form data if available
		relativePath := r.FormValue("path_" + fileHeader.Filename)
		if relativePath == "" {
			relativePath = fileName
		}

		// Clean the relative path and prevent directory traversal
		relativePath = filepath.Clean(relativePath)
		if strings.HasPrefix(relativePath, "..") {
			log.Printf("Invalid path (directory traversal attempt): %s", relativePath)
			continue
		}

		// Create full file path
		fullPath := filepath.Join(uploadDir, relativePath)
		
		// Create directories if needed
		if err := os.MkdirAll(filepath.Dir(fullPath), 0755); err != nil {
			log.Printf("Error creating directory for %s: %v", fullPath, err)
			continue
		}

		// Create the file
		dst, err := os.Create(fullPath)
		if err != nil {
			log.Printf("Error creating file %s: %v", fullPath, err)
			continue
		}
		defer dst.Close()

		// Copy file content
		written, err := io.Copy(dst, file)
		if err != nil {
			log.Printf("Error writing file %s: %v", fullPath, err)
			os.Remove(fullPath) // Clean up partial file
			continue
		}

		totalSize += written
		uploadedFiles = append(uploadedFiles, relativePath)
		log.Printf("Uploaded file: %s (%d bytes)", relativePath, written)
	}

	if len(uploadedFiles) == 0 {
		// Clean up empty directory
		os.RemoveAll(uploadDir)
		respondWithError(w, http.StatusBadRequest, "No valid files were uploaded")
		return
	}

	response := UploadResponse{
		Success:       true,
		Message:       fmt.Sprintf("Successfully uploaded %d files (%d bytes total)", len(uploadedFiles), totalSize),
		DirectoryID:   dirID,
		UploadedFiles: uploadedFiles,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
	
	log.Printf("Codebase upload completed: Directory ID %s, %d files, %d bytes", dirID, len(uploadedFiles), totalSize)
}

// listUploadedCodebases returns a list of all uploaded codebases
func listUploadedCodebases(w http.ResponseWriter, r *http.Request) {
	entries, err := os.ReadDir(BaseUploadDir)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Failed to read upload directory")
		return
	}

	var codebases []map[string]interface{}
	
	for _, entry := range entries {
		if entry.IsDir() {
			dirPath := filepath.Join(BaseUploadDir, entry.Name())
			
			// Get directory info
			info, err := entry.Info()
			if err != nil {
				continue
			}

			// Count files in directory
			fileCount := countFilesRecursively(dirPath)
			
			codebase := map[string]interface{}{
				"directory_id": entry.Name(),
				"created_at":   info.ModTime(),
				"file_count":   fileCount,
			}
			codebases = append(codebases, codebase)
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success":   true,
		"codebases": codebases,
	})
}

// getCodebaseFiles returns the file structure of a specific codebase
func getCodebaseFiles(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	dirID := vars["id"]
	
	// Validate UUID format
	if _, err := uuid.Parse(dirID); err != nil {
		respondWithError(w, http.StatusBadRequest, "Invalid directory ID")
		return
	}
	
	dirPath := filepath.Join(BaseUploadDir, dirID)
	
	// Check if directory exists
	if _, err := os.Stat(dirPath); os.IsNotExist(err) {
		respondWithError(w, http.StatusNotFound, "Codebase not found")
		return
	}
	
	files, err := getFileTree(dirPath, dirPath)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Failed to read codebase files")
		return
	}
	
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success":      true,
		"directory_id": dirID,
		"files":        files,
	})
}

// Helper function to count files recursively
func countFilesRecursively(dirPath string) int {
	count := 0
	filepath.WalkDir(dirPath, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if !d.IsDir() {
			count++
		}
		return nil
	})
	return count
}

// Helper function to get file tree
func getFileTree(basePath, currentPath string) ([]FileInfo, error) {
	var files []FileInfo
	
	entries, err := os.ReadDir(currentPath)
	if err != nil {
		return nil, err
	}
	
	for _, entry := range entries {
		fullPath := filepath.Join(currentPath, entry.Name())
		relativePath, _ := filepath.Rel(basePath, fullPath)
		
		info, err := entry.Info()
		if err != nil {
			continue
		}
		
		if entry.IsDir() {
			// Recursively get files from subdirectory
			subFiles, err := getFileTree(basePath, fullPath)
			if err == nil {
				files = append(files, subFiles...)
			}
		} else {
			files = append(files, FileInfo{
				Name: entry.Name(),
				Size: info.Size(),
				Path: relativePath,
			})
		}
	}
	
	return files, nil
}

// Helper function to send error responses
func respondWithError(w http.ResponseWriter, code int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(UploadResponse{
		Success: false,
		Message: message,
	})
}

func main() {
	r := mux.NewRouter()
	
	// Apply CORS middleware
	r.Use(enableCORS)
	
	// Routes
	r.HandleFunc("/upload", uploadCodebase).Methods("POST", "OPTIONS")
	r.HandleFunc("/codebases", listUploadedCodebases).Methods("GET")
	r.HandleFunc("/codebases/{id}", getCodebaseFiles).Methods("GET")
	
	// Health check
	r.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
	}).Methods("GET")
	
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	
	log.Printf("Server starting on port %s", port)
	log.Printf("Upload directory: %s", BaseUploadDir)
	log.Fatal(http.ListenAndServe(":"+port, r))
}