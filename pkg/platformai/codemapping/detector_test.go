package codemapping_test

import (
	"testing"

	"github.com/philipsahli/innominatus-ai-sdk/pkg/platformai/codemapping"
)

func TestDetectLanguage(t *testing.T) {
	tests := []struct {
		name     string
		analysis *codemapping.RepositoryAnalysis
		want     string
	}{
		{"nil analysis", nil, "unknown"},
		{"no files", &codemapping.RepositoryAnalysis{}, "unknown"},
		{"go.mod", files("main.go", "go.mod"), "go"},
		{"package.json", files("package.json"), "nodejs"},
		{"requirements.txt", files("requirements.txt"), "python"},
		{"pyproject.toml", files("pyproject.toml"), "python"},
		{"setup.py", files("setup.py"), "python"},
		{"Cargo.toml", files("Cargo.toml"), "rust"},
		{"pom.xml", files("pom.xml"), "java"},
		{"build.gradle", files("build.gradle"), "java"},
		{"marker beats extension count", files("a.py", "b.py", "c.py", "go.mod"), "go"},
		{"most common extension", files("a.py", "b.py", "c.go"), "python"},
		{"typescript", files("src/a.ts", "src/b.tsx"), "nodejs"},
		{"kotlin", files("App.kt"), "kotlin"},
		{"ruby", files("app.rb"), "ruby"},
		{"php", files("index.php"), "php"},
		{"rust by extension", files("src/main.rs"), "rust"},
		{"unmapped extensions only", files("README.md", "Makefile"), "unknown"},
		// Pins today's behaviour, see #28: only root-level markers count.
		{"nested marker is ignored (#28)", files("sub/go.mod"), "unknown"},
	}

	d := codemapping.NewDetector()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := d.DetectLanguage(tt.analysis); got != tt.want {
				t.Errorf("DetectLanguage() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestDetectFramework(t *testing.T) {
	tests := []struct {
		name     string
		analysis *codemapping.RepositoryAnalysis
		want     string
	}{
		{"nil analysis", nil, "none"},
		{"nothing", &codemapping.RepositoryAnalysis{}, "none"},
		{"gin", deps("github.com/gin-gonic/gin"), "gin"},
		{"echo", deps("github.com/labstack/echo"), "echo"},
		{"fiber", deps("github.com/gofiber/fiber"), "fiber"},
		{"chi", deps("github.com/go-chi/chi"), "chi"},
		{"gorilla mux", deps("github.com/gorilla/mux"), "gorilla-mux"},
		{"express", deps("express"), "express"},
		{"nestjs core", deps("@nestjs/core"), "nestjs"},
		{"nestjs common", deps("@nestjs/common"), "nestjs"},
		{"fastify", deps("fastify"), "fastify"},
		{"next", deps("next", "react"), "nextjs"},
		{"react", deps("react"), "react"},
		{"vue", deps("vue"), "vue"},
		{"fastapi", deps("fastapi"), "fastapi"},
		{"flask", deps("flask"), "flask"},
		{"django", deps("django"), "django"},
		{"Django", deps("Django"), "django"},
		{"unrelated deps", deps("lodash"), "none"},
		{"next.config.js", files("next.config.js"), "nextjs"},
		{"next.config.ts", files("web/next.config.ts"), "nextjs"},
		{"nuxt.config.js", files("nuxt.config.js"), "nuxtjs"},
		{"nuxt.config.ts", files("nuxt.config.ts"), "nuxtjs"},
		{"vue.config.js", files("vue.config.js"), "vue"},
		{"angular.json", files("frontend/angular.json"), "angular"},
		{
			"dependency beats config file",
			&codemapping.RepositoryAnalysis{
				Files:        []string{"angular.json"},
				Dependencies: map[string]string{"express": "^4"},
			},
			"express",
		},
		{"echo v4 path", deps("github.com/labstack/echo/v4"), "echo"},
		{"fiber v2 path", deps("github.com/gofiber/fiber/v2"), "fiber"},
		{"chi v5 path", deps("github.com/go-chi/chi/v5"), "chi"},
		{"gin two-digit major", deps("github.com/gin-gonic/gin/v10"), "gin"},
		{"echo-contrib alone", deps("github.com/labstack/echo-contrib"), "none"},
		{"chi prefix only", deps("github.com/go-chi/chi-extra"), "none"},
		{"bare /v suffix", deps("github.com/labstack/echo/v"), "none"},
		{"non-numeric major", deps("github.com/labstack/echo/vx"), "none"},
		{"subpackage path", deps("github.com/labstack/echo/v4/middleware"), "none"},
		{"order kept: gin before echo", deps("github.com/labstack/echo/v4", "github.com/gin-gonic/gin"), "gin"},
	}

	d := codemapping.NewDetector()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := d.DetectFramework(tt.analysis); got != tt.want {
				t.Errorf("DetectFramework() = %q, want %q", got, tt.want)
			}
		})
	}
}

func files(names ...string) *codemapping.RepositoryAnalysis {
	return &codemapping.RepositoryAnalysis{Files: names, Dependencies: map[string]string{}}
}

func deps(names ...string) *codemapping.RepositoryAnalysis {
	m := make(map[string]string, len(names))
	for _, n := range names {
		m[n] = "1.0.0"
	}
	return &codemapping.RepositoryAnalysis{Dependencies: m}
}
