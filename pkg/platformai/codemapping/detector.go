package codemapping

import (
	"path/filepath"
	"strings"
)

// Detector detects programming language and framework
type Detector struct{}

// NewDetector creates a new detector
func NewDetector() *Detector {
	return &Detector{}
}

// DetectLanguage determines the primary programming language
func (d *Detector) DetectLanguage(analysis *RepositoryAnalysis) string {
	if analysis == nil {
		return "unknown"
	}

	// Check for specific marker files
	for _, file := range analysis.Files {
		switch file {
		case "go.mod":
			return "go"
		case "package.json":
			return "nodejs"
		case "requirements.txt", "pyproject.toml", "setup.py":
			return "python"
		case "Cargo.toml":
			return "rust"
		case "pom.xml", "build.gradle":
			return "java"
		}
	}

	// Count file extensions
	extCount := make(map[string]int)
	for _, file := range analysis.Files {
		ext := strings.TrimPrefix(filepath.Ext(file), ".")
		if ext != "" {
			extCount[ext]++
		}
	}

	// Map extensions to languages
	langMapping := map[string]string{
		"go":   "go",
		"js":   "nodejs",
		"ts":   "nodejs",
		"jsx":  "nodejs",
		"tsx":  "nodejs",
		"py":   "python",
		"rs":   "rust",
		"java": "java",
		"kt":   "kotlin",
		"rb":   "ruby",
		"php":  "php",
	}

	// Find most common language
	maxCount := 0
	detectedLang := "unknown"
	for ext, count := range extCount {
		if lang, ok := langMapping[ext]; ok && count > maxCount {
			maxCount = count
			detectedLang = lang
		}
	}

	return detectedLang
}

// DetectFramework determines the framework being used
func (d *Detector) DetectFramework(analysis *RepositoryAnalysis) string {
	if analysis == nil {
		return "none"
	}

	// Go frameworks
	if hasGoModule(analysis.Dependencies, "github.com/gin-gonic/gin") {
		return "gin"
	}
	if hasGoModule(analysis.Dependencies, "github.com/labstack/echo") {
		return "echo"
	}
	if hasGoModule(analysis.Dependencies, "github.com/gofiber/fiber") {
		return "fiber"
	}
	if hasGoModule(analysis.Dependencies, "github.com/go-chi/chi") {
		return "chi"
	}
	if hasGoModule(analysis.Dependencies, "github.com/gorilla/mux") {
		return "gorilla-mux"
	}

	// Node.js frameworks
	if hasAnyDependency(analysis.Dependencies,
		"express",
	) {
		return "express"
	}
	if hasAnyDependency(analysis.Dependencies,
		"@nestjs/core", "@nestjs/common",
	) {
		return "nestjs"
	}
	if hasAnyDependency(analysis.Dependencies,
		"fastify",
	) {
		return "fastify"
	}
	if hasAnyDependency(analysis.Dependencies,
		"next",
	) {
		return "nextjs"
	}
	if hasAnyDependency(analysis.Dependencies,
		"react",
	) {
		return "react"
	}
	if hasAnyDependency(analysis.Dependencies,
		"vue",
	) {
		return "vue"
	}

	// Python frameworks
	if hasAnyDependency(analysis.Dependencies,
		"fastapi",
	) {
		return "fastapi"
	}
	if hasAnyDependency(analysis.Dependencies,
		"flask",
	) {
		return "flask"
	}
	if hasAnyDependency(analysis.Dependencies,
		"django", "Django",
	) {
		return "django"
	}

	// Check for framework indicators in files
	for _, file := range analysis.Files {
		fileName := filepath.Base(file)
		switch fileName {
		case "next.config.js", "next.config.ts":
			return "nextjs"
		case "nuxt.config.js", "nuxt.config.ts":
			return "nuxtjs"
		case "vue.config.js":
			return "vue"
		case "angular.json":
			return "angular"
		}
	}

	return "none"
}

// hasGoModule reports whether deps contain modulePath or one of its
// major-version paths (modulePath/v2, /v10, ...). Go modules from v2 on
// carry the major version in their import path.
func hasGoModule(deps map[string]string, modulePath string) bool {
	for dep := range deps {
		if dep == modulePath {
			return true
		}
		if major, ok := strings.CutPrefix(dep, modulePath+"/v"); ok && isDigits(major) {
			return true
		}
	}
	return false
}

func isDigits(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// hasAnyDependency checks if any of the given dependencies exist
func hasAnyDependency(deps map[string]string, names ...string) bool {
	for _, name := range names {
		if _, ok := deps[name]; ok {
			return true
		}
	}
	return false
}
