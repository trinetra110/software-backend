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
	MaxUploadSize = 100 << 20
	BaseStorageDir = "./storage"
)

type StorageServer struct{}

type StoreResponse struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
}

func init() {
	if err := os.MkdirAll(BaseStorageDir, 0755); err != nil {
		log.Fatalf("Failed to create base storage directory: %v", err)
	}
}

func (s *StorageServer) storeFiles(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, MaxUploadSize)
	
	if err := r.ParseMultipartForm(MaxUploadSize); err != nil {
		respondWithError(w, http.StatusBadRequest, "File too large or invalid form data")
		return
	}

	codebaseID := r.FormValue("codebase_id")
	if codebaseID == "" {
		respondWithError(w, http.StatusBadRequest, "Codebase ID is required")
		return
	}

	if _, err := uuid.Parse(codebaseID); err != nil {
		respondWithError(w, http.StatusBadRequest, "Invalid codebase ID")
		return
	}

	files := r.MultipartForm.File["files"]
	if len(files) == 0 {
		respondWithError(w, http.StatusBadRequest, "No files provided")
		return
	}

	storageDir := filepath.Join(BaseStorageDir, codebaseID)
	if err := os.MkdirAll(storageDir, 0755); err != nil {
		respondWithError(w, http.StatusInternalServerError, "Failed to create storage directory")
		return
	}

	var storedFiles []string
	var totalSize int64

	for _, fileHeader := range files {
		file, err := fileHeader.Open()
		if err != nil {
			log.Printf("Error opening file %s: %v", fileHeader.Filename, err)
			continue
		}
		defer file.Close()

		fileName := filepath.Base(fileHeader.Filename)
		if fileName == "" || fileName == "." || fileName == ".." {
			log.Printf("Invalid filename: %s", fileHeader.Filename)
			continue
		}

		relativePath := r.FormValue("path_" + fileHeader.Filename)
		if relativePath == "" {
			relativePath = fileName
		}

		relativePath = filepath.Clean(relativePath)
		if strings.HasPrefix(relativePath, "..") {
			log.Printf("Invalid path (directory traversal attempt): %s", relativePath)
			continue
		}

		fullPath := filepath.Join(storageDir, relativePath)
		
		if err := os.MkdirAll(filepath.Dir(fullPath), 0755); err != nil {
			log.Printf("Error creating directory for %s: %v", fullPath, err)
			continue
		}

		dst, err := os.Create(fullPath)
		if err != nil {
			log.Printf("Error creating file %s: %v", fullPath, err)
			continue
		}
		defer dst.Close()

		written, err := io.Copy(dst, file)
		if err != nil {
			log.Printf("Error writing file %s: %v", fullPath, err)
			os.Remove(fullPath)
			continue
		}

		totalSize += written
		storedFiles = append(storedFiles, relativePath)
		log.Printf("Stored file: %s (%d bytes)", relativePath, written)
	}

	if len(storedFiles) == 0 {
		os.RemoveAll(storageDir)
		respondWithError(w, http.StatusBadRequest, "No valid files were stored")
		return
	}

	response := StoreResponse{
		Success: true,
		Message: fmt.Sprintf("Successfully stored %d files (%d bytes total)", len(storedFiles), totalSize),
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
	
	log.Printf("Files stored for codebase %s: %d files, %d bytes", codebaseID, len(storedFiles), totalSize)
}

func (s *StorageServer) getFileContent(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	codebaseID := vars["id"]
	filePath := r.URL.Query().Get("file")
	
	if _, err := uuid.Parse(codebaseID); err != nil {
		respondWithError(w, http.StatusBadRequest, "Invalid codebase ID")
		return
	}
	
	if filePath == "" {
		respondWithError(w, http.StatusBadRequest, "File path is required")
		return
	}
	
	cleanPath := filepath.Clean(filePath)
	if strings.HasPrefix(cleanPath, "..") || strings.Contains(cleanPath, "..") {
		respondWithError(w, http.StatusBadRequest, "Invalid file path")
		return
	}
	
	baseDir := filepath.Join(BaseStorageDir, codebaseID)
	fullPath := filepath.Join(baseDir, cleanPath)
	
	if !strings.HasPrefix(fullPath, baseDir) {
		respondWithError(w, http.StatusBadRequest, "Invalid file path")
		return
	}
	
	fileInfo, err := os.Stat(fullPath)
	if os.IsNotExist(err) {
		respondWithError(w, http.StatusNotFound, "File not found")
		return
	}
	
	if fileInfo.IsDir() {
		respondWithError(w, http.StatusBadRequest, "Cannot read directory as file")
		return
	}
	
	content, err := os.ReadFile(fullPath)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Failed to read file")
		return
	}
	
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
		response["download_url"] = fmt.Sprintf("/download/%s?file=%s", codebaseID, filePath)
	}
	
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

func (s *StorageServer) downloadFile(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	codebaseID := vars["id"]
	filePath := r.URL.Query().Get("file")
	
	if _, err := uuid.Parse(codebaseID); err != nil {
		respondWithError(w, http.StatusBadRequest, "Invalid codebase ID")
		return
	}
	
	if filePath == "" {
		respondWithError(w, http.StatusBadRequest, "File path is required")
		return
	}
	
	cleanPath := filepath.Clean(filePath)
	if strings.HasPrefix(cleanPath, "..") || strings.Contains(cleanPath, "..") {
		respondWithError(w, http.StatusBadRequest, "Invalid file path")
		return
	}
	
	baseDir := filepath.Join(BaseStorageDir, codebaseID)
	fullPath := filepath.Join(baseDir, cleanPath)
	
	if !strings.HasPrefix(fullPath, baseDir) {
		respondWithError(w, http.StatusBadRequest, "Invalid file path")
		return
	}
	
	fileInfo, err := os.Stat(fullPath)
	if os.IsNotExist(err) {
		respondWithError(w, http.StatusNotFound, "File not found")
		return
	}
	
	if fileInfo.IsDir() {
		respondWithError(w, http.StatusBadRequest, "Cannot download directory")
		return
	}
	
	file, err := os.Open(fullPath)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Failed to open file")
		return
	}
	defer file.Close()
	
	filename := filepath.Base(cleanPath)
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=\"%s\"", filename))
	w.Header().Set("Content-Length", fmt.Sprintf("%d", fileInfo.Size()))
	
	_, err = io.Copy(w, file)
	if err != nil {
		log.Printf("Error streaming file %s: %v", fullPath, err)
		return
	}
	
	log.Printf("Downloaded file: %s from codebase %s", cleanPath, codebaseID)
}

func (s *StorageServer) downloadZip(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	codebaseID := vars["id"]
	
	if _, err := uuid.Parse(codebaseID); err != nil {
		respondWithError(w, http.StatusBadRequest, "Invalid codebase ID")
		return
	}
	
	storageDir := filepath.Join(BaseStorageDir, codebaseID)
	
	if _, err := os.Stat(storageDir); os.IsNotExist(err) {
		respondWithError(w, http.StatusNotFound, "Codebase not found")
		return
	}
	
	filename := fmt.Sprintf("codebase-%s.zip", codebaseID)
	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=\"%s\"", filename))
	
	err := createZipArchive(w, storageDir)
	if err != nil {
		log.Printf("Error creating ZIP for codebase %s: %v", codebaseID, err)
		return
	}
	
	log.Printf("Downloaded ZIP archive for codebase: %s", codebaseID)
}

func createZipArchive(w io.Writer, sourceDir string) error {
	zipWriter := zip.NewWriter(w)
	defer zipWriter.Close()
	
	return filepath.WalkDir(sourceDir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		
		relativePath, err := filepath.Rel(sourceDir, path)
		if err != nil {
			return err
		}
		
		if relativePath == "." {
			return nil
		}
		
		relativePath = strings.ReplaceAll(relativePath, "\\", "/")
		
		if d.IsDir() {
			_, err := zipWriter.Create(relativePath + "/")
			return err
		}
		
		zipFile, err := zipWriter.Create(relativePath)
		if err != nil {
			return err
		}
		
		sourceFile, err := os.Open(path)
		if err != nil {
			return err
		}
		defer sourceFile.Close()
		
		_, err = io.Copy(zipFile, sourceFile)
		return err
	})
}

func isTextFile(content []byte) bool {
	if len(content) == 0 {
		return true
	}
	
	if !utf8.Valid(content) {
		return false
	}
	
	nullBytes := 0
	controlChars := 0
	
	for i, b := range content {
		if i > 8192 {
			break
		}
		
		if b == 0 {
			nullBytes++
		}
		
		if b < 32 && b != 9 && b != 10 && b != 13 {
			controlChars++
		}
	}
	
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

func respondWithError(w http.ResponseWriter, code int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": false,
		"error":   message,
	})
}

func main() {
	server := &StorageServer{}
	
	r := mux.NewRouter()
	
	// Storage routes
	r.HandleFunc("/store", server.storeFiles).Methods("POST")
	r.HandleFunc("/content/{id}", server.getFileContent).Methods("GET")
	r.HandleFunc("/download/{id}", server.downloadFile).Methods("GET")
	r.HandleFunc("/zip/{id}", server.downloadZip).Methods("GET")
	
	// Health check
	r.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
	}).Methods("GET")

	port := os.Getenv("PORT")
	if port == "" {
		port = "8081"
	}
	
	log.Printf("Storage Server B starting on port %s", port)
	log.Printf("Storage directory: %s", BaseStorageDir)
	log.Fatal(http.ListenAndServe(":"+port, r))
}