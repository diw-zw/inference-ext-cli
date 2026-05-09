/*
Copyright 2026 The RBG Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package main

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

const (
	extPNG  = ".png"
	extJPG  = ".jpg"
	extJPEG = ".jpeg"
	extGIF  = ".gif"
	extSVG  = ".svg"
)

// Server holds the HTTP server configuration and state.
type Server struct {
	dataDir string
	mux     *http.ServeMux
}

// NewServer creates a new HTTP server instance.
func NewServer(dataDir string) (*Server, error) {
	baseAbs, err := filepath.Abs(dataDir)
	if err != nil {
		return nil, err
	}
	s := &Server{
		dataDir: baseAbs,
		mux:     http.NewServeMux(),
	}
	s.registerRoutes()
	return s, nil
}

// registerRoutes sets up all HTTP routes.
func (s *Server) registerRoutes() {
	// API routes
	s.mux.HandleFunc("/api/experiments", s.handleListExperiments)
	s.mux.HandleFunc("/api/experiments/", s.handleExperimentRoutes)
	s.mux.HandleFunc("/api/compare", s.handleCompare)

	// Static files - serve embedded UI
	s.mux.HandleFunc("/", s.handleStaticFiles)
}

// ServeHTTP implements the http.Handler interface.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.mux.ServeHTTP(w, r)
}

// handleListExperiments returns a list of all experiments.
func (s *Server) handleListExperiments(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		s.writeError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	experiments, err := s.listExperiments()
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, fmt.Sprintf("Failed to list experiments: %v", err))
		return
	}

	resp := ExperimentListResponse{
		Experiments: experiments,
		Total:       len(experiments),
	}

	s.writeJSON(w, http.StatusOK, resp)
}

// handleExperimentRoutes routes requests to specific experiment handlers.
func (s *Server) handleExperimentRoutes(w http.ResponseWriter, r *http.Request) {
	// Parse path: /api/experiments/{name}[/results[/{file}]][/images/{file}]
	path := strings.TrimPrefix(r.URL.Path, "/api/experiments/")
	parts := strings.SplitN(path, "/", 3)

	if len(parts) == 0 || parts[0] == "" {
		s.writeError(w, http.StatusBadRequest, "Experiment name is required")
		return
	}

	experimentName := parts[0]

	if len(parts) == 1 {
		// GET /api/experiments/{name}
		s.handleGetExperiment(w, r, experimentName)
		return
	}

	switch parts[1] {
	case "results":
		if len(parts) == 2 {
			// GET /api/experiments/{name}/results
			s.handleListResults(w, r, experimentName)
		} else {
			// GET /api/experiments/{name}/results/{file}
			s.handleGetResult(w, r, experimentName, parts[2])
		}
	case "images":
		if len(parts) < 3 {
			s.writeError(w, http.StatusBadRequest, "Image filename is required")
			return
		}
		// GET /api/experiments/{name}/images/{file}
		s.handleGetImage(w, r, experimentName, parts[2])
	default:
		s.writeError(w, http.StatusNotFound, "Unknown endpoint")
	}
}

// handleGetExperiment returns details of a specific experiment.
func (s *Server) handleGetExperiment(w http.ResponseWriter, r *http.Request, name string) {
	if r.Method != http.MethodGet {
		s.writeError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	experiment, err := s.getExperiment(name)
	if err != nil {
		if os.IsNotExist(err) {
			s.writeError(w, http.StatusNotFound, "Experiment not found")
			return
		}
		s.writeError(w, http.StatusInternalServerError, fmt.Sprintf("Failed to get experiment: %v", err))
		return
	}

	s.writeJSON(w, http.StatusOK, experiment)
}

// handleListResults returns a list of result summaries for an experiment.
func (s *Server) handleListResults(w http.ResponseWriter, r *http.Request, experimentName string) {
	if r.Method != http.MethodGet {
		s.writeError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	summaries, err := s.getResultSummaries(experimentName)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, fmt.Sprintf("Failed to get results: %v", err))
		return
	}

	s.writeJSON(w, http.StatusOK, summaries)
}

// handleGetResult returns a specific result file.
func (s *Server) handleGetResult(w http.ResponseWriter, r *http.Request, experimentName, fileName string) {
	if r.Method != http.MethodGet {
		s.writeError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	if strings.Contains(experimentName, "/") || strings.Contains(experimentName, "\\") || strings.Contains(experimentName, "..") {
		s.writeError(w, http.StatusBadRequest, "Invalid experiment name")
		return
	}
	if strings.Contains(fileName, "/") || strings.Contains(fileName, "\\") || strings.Contains(fileName, "..") {
		s.writeError(w, http.StatusBadRequest, "Invalid result filename")
		return
	}

	// Ensure only JSON result files are returned and exclude experiment metadata.
	lowerName := strings.ToLower(fileName)
	if !strings.HasSuffix(lowerName, ".json") {
		s.writeError(w, http.StatusBadRequest, "Only JSON result files can be requested")
		return
	}
	if strings.EqualFold(fileName, "experiment_metadata.json") {
		s.writeError(w, http.StatusForbidden, "Access to experiment metadata is not allowed")
		return
	}

	// Local safety check: validate path is within dataDir
	// This provides explicit validation that static analysis tools can recognize
	expDir, ok := isSafeRelativePath(s.dataDir, experimentName)
	if !ok {
		s.writeError(w, http.StatusForbidden, "Access denied")
		return
	}

	// Validate fileName is safe and construct the final file path
	filePath, ok := isSafeRelativePath(expDir, fileName)
	if !ok {
		s.writeError(w, http.StatusForbidden, "Access denied")
		return
	}

	data, err := os.ReadFile(filePath)
	if err != nil {
		if os.IsNotExist(err) {
			s.writeError(w, http.StatusNotFound, "Result file not found")
			return
		}
		s.writeError(w, http.StatusInternalServerError, fmt.Sprintf("Failed to read result: %v", err))
		return
	}

	// Validate that the content is valid JSON
	var jsonData interface{}
	if err := json.Unmarshal(data, &jsonData); err != nil {
		s.writeError(w, http.StatusBadRequest, "Invalid JSON content in result file")
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(data)
}

// handleGetImage serves an image file from an experiment.
func (s *Server) handleGetImage(w http.ResponseWriter, r *http.Request, experimentName, fileName string) {
	if r.Method != http.MethodGet {
		s.writeError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}
	if strings.Contains(experimentName, "/") || strings.Contains(experimentName, "\\") || strings.Contains(experimentName, "..") {
		s.writeError(w, http.StatusBadRequest, "Invalid experiment name")
		return
	}
	if strings.Contains(fileName, "/") || strings.Contains(fileName, "\\") || strings.Contains(fileName, "..") {
		s.writeError(w, http.StatusBadRequest, "Invalid result filename")
		return
	}

	// Validate that the file has an image extension
	ext := strings.ToLower(filepath.Ext(fileName))
	switch ext {
	case extPNG, extJPG, extJPEG, extGIF, extSVG:
		// Allowed image extensions
	default:
		s.writeError(w, http.StatusBadRequest, "Invalid image format. Only png, jpg, jpeg, gif, svg are allowed")
		return
	}

	// Local safety check: validate path is within dataDir
	// This provides explicit validation that static analysis tools can recognize
	expDir, ok := isSafeRelativePath(s.dataDir, experimentName)
	if !ok {
		s.writeError(w, http.StatusForbidden, "Access denied")
		return
	}

	// Validate fileName is safe and construct the final file path
	filePath, ok := isSafeRelativePath(expDir, fileName)
	if !ok {
		s.writeError(w, http.StatusForbidden, "Access denied")
		return
	}

	// Determine content type
	contentType := "application/octet-stream"
	switch ext {
	case extPNG:
		contentType = "image/png"
	case extJPG, extJPEG:
		contentType = "image/jpeg"
	case extGIF:
		contentType = "image/gif"
	case extSVG:
		contentType = "image/svg+xml"
	}

	data, err := os.ReadFile(filePath)
	if err != nil {
		if os.IsNotExist(err) {
			s.writeError(w, http.StatusNotFound, "Image not found")
			return
		}
		s.writeError(w, http.StatusInternalServerError, fmt.Sprintf("Failed to read image: %v", err))
		return
	}

	w.Header().Set("Content-Type", contentType)
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(data)
}

// handleCompare compares multiple experiments.
func (s *Server) handleCompare(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodPost {
		s.writeError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	var experimentNames []string

	if r.Method == http.MethodGet {
		// Get experiment names from query parameter
		names := r.URL.Query().Get("experiments")
		if names == "" {
			s.writeError(w, http.StatusBadRequest, "experiments parameter is required")
			return
		}
		experimentNames = strings.Split(names, ",")
	} else {
		// POST with JSON body
		var req CompareRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			s.writeError(w, http.StatusBadRequest, "Invalid request body")
			return
		}
		experimentNames = req.ExperimentNames
	}

	if len(experimentNames) < 1 {
		s.writeError(w, http.StatusBadRequest, "At least one experiment is required for comparison")
		return
	}

	compareData := make([]ExperimentCompareData, 0, len(experimentNames))
	for _, name := range experimentNames {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}

		experiment, err := s.getExperiment(name)
		if err != nil {
			continue // Skip experiments that don't exist
		}

		summaries, err := s.getResultSummaries(name)
		if err != nil {
			continue
		}

		compareData = append(compareData, ExperimentCompareData{
			Name:     name,
			Metadata: experiment.Metadata,
			Results:  summaries,
		})
	}

	s.writeJSON(w, http.StatusOK, CompareResponse{Experiments: compareData})
}

// handleStaticFiles serves the embedded static files.
func (s *Server) handleStaticFiles(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Path
	if path == "/" {
		path = "/index.html"
	}

	// Try to serve from embedded files
	content, contentType, err := getStaticFile(path)
	if err != nil {
		// Serve index.html for SPA routing
		if path != "/index.html" {
			content, contentType, err = getStaticFile("/index.html")
			if err != nil {
				s.writeError(w, http.StatusNotFound, "Page not found")
				return
			}
		} else {
			s.writeError(w, http.StatusNotFound, "Page not found")
			return
		}
	}

	w.Header().Set("Content-Type", contentType)
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(content)
}

// listExperiments returns all experiments in the data directory.
func (s *Server) listExperiments() ([]Experiment, error) {
	entries, err := os.ReadDir(s.dataDir)
	if err != nil {
		return nil, err
	}

	experiments := make([]Experiment, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		exp, err := s.getExperiment(entry.Name())
		if err != nil {
			// Skip directories that don't look like experiments
			continue
		}
		experiments = append(experiments, *exp)
	}

	// Sort by creation time, newest first
	sort.Slice(experiments, func(i, j int) bool {
		return experiments[i].CreatedAt.After(experiments[j].CreatedAt)
	})

	return experiments, nil
}

// getExperiment returns details of a specific experiment.
func (s *Server) getExperiment(name string) (*Experiment, error) {
	if strings.Contains(name, "/") || strings.Contains(name, "\\") || strings.Contains(name, "..") {
		return nil, os.ErrNotExist
	}
	// Resolve symlinks for both the base directory and the experiment directory
	// before checking containment, so a symlink under s.dataDir cannot escape it.
	baseDir, err := filepath.EvalSymlinks(s.dataDir)
	if err != nil {
		return nil, err
	}
	baseDir, err = filepath.Abs(baseDir)
	if err != nil {
		return nil, err
	}
	expDirJoined := filepath.Join(s.dataDir, name)
	expDir, err := filepath.EvalSymlinks(expDirJoined)
	if err != nil {
		return nil, err
	}
	expDir, err = filepath.Abs(expDir)
	if err != nil {
		return nil, err
	}
	relPath, err := filepath.Rel(baseDir, expDir)
	if err != nil {
		return nil, err
	}
	if relPath == ".." || strings.HasPrefix(relPath, ".."+string(os.PathSeparator)) {
		return nil, os.ErrNotExist
	}
	info, err := os.Stat(expDir)
	if err != nil {
		return nil, err
	}

	exp := &Experiment{
		Name:      name,
		CreatedAt: info.ModTime(),
	}

	// Read metadata
	metadataPath := filepath.Join(expDir, "experiment_metadata.json")
	if data, err := os.ReadFile(metadataPath); err == nil {
		var metadata ExperimentMetadata
		if json.Unmarshal(data, &metadata) == nil {
			exp.Metadata = &metadata
		}
	}

	// List files
	entries, err := os.ReadDir(expDir)
	if err != nil {
		return nil, err
	}

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		ext := strings.ToLower(filepath.Ext(name))

		switch {
		case ext == ".json" && name != "experiment_metadata.json":
			exp.ResultFiles = append(exp.ResultFiles, name)
		case ext == extPNG || ext == extJPG || ext == extJPEG || ext == extGIF || ext == extSVG:
			exp.ImageFiles = append(exp.ImageFiles, name)
		case ext == ".xlsx" || ext == ".xls":
			exp.ExcelFiles = append(exp.ExcelFiles, name)
		}
	}

	return exp, nil
}

// getResultSummaries returns summaries of all result files in an experiment.
func (s *Server) getResultSummaries(experimentName string) ([]ResultSummary, error) {
	if strings.Contains(experimentName, "/") || strings.Contains(experimentName, "\\") || strings.Contains(experimentName, "..") {
		return nil, os.ErrNotExist
	}

	// Local safety check: validate path is within dataDir
	// This provides explicit validation that static analysis tools can recognize
	expDir, ok := isSafeRelativePath(s.dataDir, experimentName)
	if !ok {
		return nil, os.ErrNotExist
	}

	entries, err := os.ReadDir(expDir)
	if err != nil {
		return nil, err
	}

	summaries := make([]ResultSummary, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if !strings.HasSuffix(name, ".json") || name == "experiment_metadata.json" {
			continue
		}

		filePath := filepath.Join(expDir, name)
		data, err := os.ReadFile(filePath)
		if err != nil {
			continue
		}

		var result BenchmarkResult
		if err := json.Unmarshal(data, &result); err != nil {
			continue
		}

		metrics := result.AggregatedMetrics
		summary := ResultSummary{
			FileName:          name,
			Scenario:          metrics.Scenario,
			NumConcurrency:    metrics.NumConcurrency,
			RunDuration:       metrics.RunDuration,
			RequestsPerSecond: metrics.RequestsPerSecond,
			OutputThroughput:  metrics.MeanOutputThroughput,
			ErrorRate:         metrics.ErrorRate,
			NumRequests:       metrics.NumRequests,
		}

		// Extract stats if available
		if ttftStats, ok := metrics.Stats["ttft"]; ok {
			summary.MeanTTFT = ttftStats.Mean
			summary.P99TTFT = ttftStats.P99
		}
		if e2eStats, ok := metrics.Stats["e2e_latency"]; ok {
			summary.MeanE2ELatency = e2eStats.Mean
			summary.P99E2ELatency = e2eStats.P99
		}

		summaries = append(summaries, summary)
	}

	// Sort by scenario and concurrency
	sort.Slice(summaries, func(i, j int) bool {
		if summaries[i].Scenario != summaries[j].Scenario {
			return summaries[i].Scenario < summaries[j].Scenario
		}
		return summaries[i].NumConcurrency < summaries[j].NumConcurrency
	})

	return summaries, nil
}

// isValidName checks if the name contains only safe characters.
// Allows: alphanumeric, hyphen, underscore, dot
// Rejects: path separators, parent directory references, etc.
func isValidName(name string) bool {
	if name == "" || name == "." || name == ".." {
		return false
	}
	// Only allow safe characters: alphanumeric, hyphen, underscore, dot
	for _, r := range name {
		if !((r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') ||
			(r >= '0' && r <= '9') || r == '-' || r == '_' || r == '.') {
			return false
		}
	}
	return true
}

// isSafeRelativePath validates that subpath is safe relative to baseDir and returns the absolute path.
// It checks that the joined and normalized path is still within baseDir.
// This provides a local, explicit safety check that static analysis tools can recognize.
func isSafeRelativePath(absBaseDir, subpath string) (string, bool) {
	// First validate the subpath contains only safe characters
	if !isValidName(subpath) {
		return "", false
	}

	// Join and clean the path
	joined := filepath.Join(absBaseDir, subpath)
	absPath, err := filepath.Abs(joined)
	if err != nil {
		return "", false
	}

	// Resolve symlinks for both base dir and target path
	// This prevents symlink-based path traversal attacks
	evalBaseDir, err := filepath.EvalSymlinks(absBaseDir)
	if err != nil {
		return "", false
	}

	// For the target path, we need to handle the case where it may not exist yet
	// Try to evaluate symlinks, but if the path doesn't exist, we'll validate
	// against the parent directory
	evalPath, err := filepath.EvalSymlinks(absPath)
	if err != nil {
		// Path doesn't exist - validate against base dir
		// Use filepath.Rel to ensure the path is relative to base dir
		relPath, err := filepath.Rel(absBaseDir, absPath)
		if err != nil {
			return "", false
		}
		// Check if relative path escapes base dir
		if strings.HasPrefix(relPath, "..") || relPath == ".." {
			return "", false
		}
		return absPath, true
	}

	// Path exists - verify it's within the resolved base dir
	relPath, err := filepath.Rel(evalBaseDir, evalPath)
	if err != nil {
		return "", false
	}
	// Check if relative path escapes base dir
	if strings.HasPrefix(relPath, "..") || relPath == ".." {
		return "", false
	}

	return evalPath, true
}

// writeJSON writes a JSON response.
func (s *Server) writeJSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(data)
}

// writeError writes an error response.
func (s *Server) writeError(w http.ResponseWriter, status int, message string) {
	s.writeJSON(w, status, ErrorResponse{
		Error:   http.StatusText(status),
		Message: message,
	})
}

// getStaticFile returns the content and content type for a static file.
func getStaticFile(path string) ([]byte, string, error) {
	path = strings.TrimPrefix(path, "/")

	content, err := fs.ReadFile(staticFS, "static/"+path)
	if err != nil {
		return nil, "", err
	}

	contentType := "text/html; charset=utf-8"
	ext := strings.ToLower(filepath.Ext(path))
	switch ext {
	case ".css":
		contentType = "text/css; charset=utf-8"
	case ".js":
		contentType = "application/javascript; charset=utf-8"
	case ".json":
		contentType = "application/json"
	case extPNG:
		contentType = "image/png"
	case extJPG, extJPEG:
		contentType = "image/jpeg"
	case extSVG:
		contentType = "image/svg+xml"
	case ".ico":
		contentType = "image/x-icon"
	}

	return content, contentType, nil
}
