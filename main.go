package main

import (
	"archive/zip"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"

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

// downloadFile downloads a specific file from a codebase
func downloadFile(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	dirID := vars["id"]
	filePath := r.URL.Query().Get("file")
	
	// Validate UUID format
	if _, err := uuid.Parse(dirID); err != nil {
		respondWithError(w, http.StatusBadRequest, "Invalid directory ID")
		return
	}
	
	if filePath == "" {
		respondWithError(w, http.StatusBadRequest, "File path is required")
		return
	}
	
	// Clean the file path and prevent directory traversal
	cleanPath := filepath.Clean(filePath)
	if strings.HasPrefix(cleanPath, "..") || strings.Contains(cleanPath, "..") {
		respondWithError(w, http.StatusBadRequest, "Invalid file path")
		return
	}
	
	// Construct full file path
	baseDir := filepath.Join(BaseUploadDir, dirID)
	fullPath := filepath.Join(baseDir, cleanPath)
	
	// Ensure the file is within the codebase directory
	if !strings.HasPrefix(fullPath, baseDir) {
		respondWithError(w, http.StatusBadRequest, "Invalid file path")
		return
	}
	
	// Check if file exists
	fileInfo, err := os.Stat(fullPath)
	if os.IsNotExist(err) {
		respondWithError(w, http.StatusNotFound, "File not found")
		return
	}
	
	if fileInfo.IsDir() {
		respondWithError(w, http.StatusBadRequest, "Cannot download directory")
		return
	}
	
	// Open the file
	file, err := os.Open(fullPath)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Failed to open file")
		return
	}
	defer file.Close()
	
	// Set headers for file download
	filename := filepath.Base(cleanPath)
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=\"%s\"", filename))
	w.Header().Set("Content-Length", fmt.Sprintf("%d", fileInfo.Size()))
	
	// Copy file content to response
	_, err = io.Copy(w, file)
	if err != nil {
		log.Printf("Error streaming file %s: %v", fullPath, err)
		return
	}
	
	log.Printf("Downloaded file: %s from codebase %s", cleanPath, dirID)
}

// downloadCodebaseZip downloads the entire codebase as a ZIP file
func downloadCodebaseZip(w http.ResponseWriter, r *http.Request) {
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
	
	// Set headers for ZIP download
	filename := fmt.Sprintf("codebase-%s.zip", dirID)
	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=\"%s\"", filename))
	
	// Create ZIP archive directly to response writer
	err := createZipArchive(w, dirPath)
	if err != nil {
		log.Printf("Error creating ZIP for codebase %s: %v", dirID, err)
		// Can't send error response here as headers are already sent
		return
	}
	
	log.Printf("Downloaded ZIP archive for codebase: %s", dirID)
}

// readFileContent returns the content of a specific file
func readFileContent(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	dirID := vars["id"]
	filePath := r.URL.Query().Get("file")
	
	// Validate UUID format
	if _, err := uuid.Parse(dirID); err != nil {
		respondWithError(w, http.StatusBadRequest, "Invalid directory ID")
		return
	}
	
	if filePath == "" {
		respondWithError(w, http.StatusBadRequest, "File path is required")
		return
	}
	
	// Clean the file path and prevent directory traversal
	cleanPath := filepath.Clean(filePath)
	if strings.HasPrefix(cleanPath, "..") || strings.Contains(cleanPath, "..") {
		respondWithError(w, http.StatusBadRequest, "Invalid file path")
		return
	}
	
	// Construct full file path
	baseDir := filepath.Join(BaseUploadDir, dirID)
	fullPath := filepath.Join(baseDir, cleanPath)
	
	// Ensure the file is within the codebase directory
	if !strings.HasPrefix(fullPath, baseDir) {
		respondWithError(w, http.StatusBadRequest, "Invalid file path")
		return
	}
	
	// Check if file exists and is not a directory
	fileInfo, err := os.Stat(fullPath)
	if os.IsNotExist(err) {
		respondWithError(w, http.StatusNotFound, "File not found")
		return
	}
	
	if fileInfo.IsDir() {
		respondWithError(w, http.StatusBadRequest, "Cannot read directory as file")
		return
	}
	
	// Read file content
	content, err := os.ReadFile(fullPath)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Failed to read file")
		return
	}
	
	// Determine if file is text or binary
	isText := isTextFile(content)
	
	response := map[string]interface{}{
		"success":   true,
		"file_path": cleanPath,
		"size":      fileInfo.Size(),
		"is_text":   isText,
		"modified":  fileInfo.ModTime(),
	}
	
	if isText {
		response["content"] = string(content)
	} else {
		response["content"] = "Binary file - use download endpoint to get the file"
		response["download_url"] = fmt.Sprintf("/codebases/%s/download?file=%s", dirID, filePath)
	}
	
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
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

// Helper function to create ZIP archive
func createZipArchive(w io.Writer, sourceDir string) error {
	zipWriter := zip.NewWriter(w)
	defer zipWriter.Close()
	
	return filepath.WalkDir(sourceDir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		
		// Get relative path for the zip entry
		relativePath, err := filepath.Rel(sourceDir, path)
		if err != nil {
			return err
		}
		
		// Skip the root directory itself
		if relativePath == "." {
			return nil
		}
		
		// Convert to forward slashes for ZIP compatibility
		relativePath = strings.ReplaceAll(relativePath, "\\", "/")
		
		if d.IsDir() {
			// Create directory entry in ZIP
			_, err := zipWriter.Create(relativePath + "/")
			return err
		}
		
		// Create file entry in ZIP
		zipFile, err := zipWriter.Create(relativePath)
		if err != nil {
			return err
		}
		
		// Open source file
		sourceFile, err := os.Open(path)
		if err != nil {
			return err
		}
		defer sourceFile.Close()
		
		// Copy file content to ZIP
		_, err = io.Copy(zipFile, sourceFile)
		return err
	})
}

// Helper function to check if content is text
func isTextFile(content []byte) bool {
	if len(content) == 0 {
		return true
	}
	
	// Check if content is valid UTF-8
	if !utf8.Valid(content) {
		return false
	}
	
	// Check for binary indicators (null bytes, excessive control characters)
	nullBytes := 0
	controlChars := 0
	
	for i, b := range content {
		if i > 8192 { // Check only first 8KB
			break
		}
		
		if b == 0 {
			nullBytes++
		}
		
		if b < 32 && b != 9 && b != 10 && b != 13 { // Tab, LF, CR are OK
			controlChars++
		}
	}
	
	// If more than 1% null bytes or 5% control chars, consider binary
	contentLen := len(content)
	if contentLen > 100 {
		if float64(nullBytes)/float64(contentLen) > 0.01 {
			return false
		}
		if float64(controlChars)/float64(contentLen) > 0.05 {
			return false
		}
	}
	
	return true
}

func main() {
	r := mux.NewRouter()
	
	// Apply CORS middleware
	r.Use(enableCORS)
	
	// Routes
	r.HandleFunc("/upload", uploadCodebase).Methods("POST", "OPTIONS")
	r.HandleFunc("/codebases", listUploadedCodebases).Methods("GET")
	r.HandleFunc("/codebases/{id}", getCodebaseFiles).Methods("GET")
	r.HandleFunc("/codebases/{id}/content", readFileContent).Methods("GET")
	r.HandleFunc("/codebases/{id}/download", downloadFile).Methods("GET")
	r.HandleFunc("/codebases/{id}/zip", downloadCodebaseZip).Methods("GET")
	
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

func respondWithError(w http.ResponseWriter, code int, message string) {
    w.Header().Set("Content-Type", "application/json")
    w.WriteHeader(code)
    json.NewEncoder(w).Encode(map[string]interface{}{
        "success": false,
        "error":   message,
    })
}