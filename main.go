package main

import (
	_ "embed"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

//go:embed index.html
var indexHTML string

var audioDir string

type AudioFile struct {
	Name   string `json:"name"`
	Path   string `json:"path"`
	Folder string `json:"folder"`
}

type PaginatedResponse struct {
	Files      []AudioFile `json:"files"`
	Page       int         `json:"page"`
	PerPage    int         `json:"perPage"`
	Total      int         `json:"total"`
	TotalPages int         `json:"totalPages"`
}

func main() {
	var port string
	var help bool

	flag.StringVar(&port, "port", "8080", "Port to serve on")
	flag.StringVar(&port, "p", "8080", "Port to serve on (shorthand)")
	flag.StringVar(&audioDir, "dir", "", "Directory to serve audio files from (default: current directory)")
	flag.StringVar(&audioDir, "d", "", "Directory to serve audio files from (shorthand)")
	flag.BoolVar(&help, "help", false, "Show help")
	flag.BoolVar(&help, "h", false, "Show help (shorthand)")

	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "🎵 Beatgraze - Web-based audio file player\n\n")
		fmt.Fprintf(os.Stderr, "Usage: %s [options] [directory]\n\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "Options:\n")
		flag.PrintDefaults()
		fmt.Fprintf(os.Stderr, "\nExamples:\n")
		fmt.Fprintf(os.Stderr, "  %s                    # Serve current directory on port 8080\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "  %s -p 3000            # Serve current directory on port 3000\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "  %s -d /path/to/music  # Serve specific directory\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "  %s /path/to/music     # Serve specific directory (positional)\n", os.Args[0])
	}

	flag.Parse()

	if help {
		flag.Usage()
		os.Exit(0)
	}

	// Handle positional argument for directory
	if flag.NArg() > 0 {
		audioDir = flag.Arg(0)
	}

	// Default to current directory if none specified
	if audioDir == "" {
		var err error
		audioDir, err = os.Getwd()
		if err != nil {
			log.Fatal("Error getting current directory:", err)
		}
	}

	// Validate directory exists
	if _, err := os.Stat(audioDir); os.IsNotExist(err) {
		log.Fatalf("Directory does not exist: %s", audioDir)
	}

	// Convert to absolute path
	var err error
	audioDir, err = filepath.Abs(audioDir)
	if err != nil {
		log.Fatal("Error resolving directory path:", err)
	}

	http.HandleFunc("/", serveIndex)
	http.HandleFunc("/api/files", getAudioFiles)
	http.HandleFunc("/audio/", serveAudio)

	fmt.Printf("🎵 Beatgraze running at http://localhost:%s\n", port)
	fmt.Printf("📁 Serving audio files from: %s\n", audioDir)
	log.Fatal(http.ListenAndServe(":"+port, nil))
}

func serveIndex(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html")
	w.Write([]byte(indexHTML))
}

func getAudioFiles(w http.ResponseWriter, r *http.Request) {
	// Parse query parameters
	pageStr := r.URL.Query().Get("page")
	perPageStr := r.URL.Query().Get("perPage")
	searchQuery := strings.TrimSpace(r.URL.Query().Get("search"))

	page := 1
	perPage := 200

	if pageStr != "" {
		if p, err := strconv.Atoi(pageStr); err == nil && p > 0 {
			page = p
		}
	}

	if perPageStr != "" {
		if pp, err := strconv.Atoi(perPageStr); err == nil && pp > 0 && pp <= 1000 {
			perPage = pp
		}
	}
	var audioFiles []AudioFile
	audioExts := map[string]bool{
		".mp3":  true,
		".wav":  true,
		".flac": true,
		".m4a":  true,
		".aac":  true,
		".ogg":  true,
	}

	err := filepath.Walk(audioDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if info.IsDir() {
			return nil
		}

		ext := strings.ToLower(filepath.Ext(path))
		if audioExts[ext] {
			relPath, _ := filepath.Rel(audioDir, path)
			folderName := filepath.Dir(relPath)
			if folderName == "." {
				folderName = "" // Root directory
			} else {
				folderName = filepath.Base(folderName) // Just the immediate parent folder name
			}
			audioFiles = append(audioFiles, AudioFile{
				Name:   info.Name(),
				Path:   relPath,
				Folder: folderName,
			})
		}
		return nil
	})

	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Filter by search query if provided
	if searchQuery != "" {
		var filteredFiles []AudioFile

		// Check for dir: syntax
		if strings.HasPrefix(searchQuery, "dir:") {
			dirFilter := strings.TrimSpace(strings.TrimPrefix(searchQuery, "dir:"))
			// Remove leading ./ if present
			dirFilter = strings.TrimPrefix(dirFilter, "./")

			for _, file := range audioFiles {
				if file.Folder == dirFilter || (dirFilter == "" && file.Folder == "") {
					filteredFiles = append(filteredFiles, file)
				}
			}
		} else {
			// Regular text search
			searchLower := strings.ToLower(searchQuery)
			for _, file := range audioFiles {
				if strings.Contains(strings.ToLower(file.Name), searchLower) ||
					strings.Contains(strings.ToLower(file.Path), searchLower) ||
					strings.Contains(strings.ToLower(file.Folder), searchLower) {
					filteredFiles = append(filteredFiles, file)
				}
			}
		}
		audioFiles = filteredFiles
	}
	// Sort files by name for consistent pagination
	sort.Slice(audioFiles, func(i, j int) bool {
		return audioFiles[i].Name < audioFiles[j].Name
	})

	total := len(audioFiles)
	totalPages := (total + perPage - 1) / perPage

	// Calculate pagination bounds
	start := (page - 1) * perPage
	end := start + perPage

	if start >= total {
		// Page out of range, return empty
		start = total
		end = total
	} else if end > total {
		end = total
	}

	paginatedFiles := audioFiles[start:end]

	response := PaginatedResponse{
		Files:      paginatedFiles,
		Page:       page,
		PerPage:    perPage,
		Total:      total,
		TotalPages: totalPages,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

func serveAudio(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/audio/")
	fullPath := filepath.Join(audioDir, path)

	// Security check: ensure the resolved path is within audioDir
	if !strings.HasPrefix(fullPath, audioDir) {
		http.Error(w, "Invalid path", http.StatusBadRequest)
		return
	}

	http.ServeFile(w, r, fullPath)
}
