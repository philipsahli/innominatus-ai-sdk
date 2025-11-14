package platformai

import "errors"

// Common error types for the Platform AI SDK
var (
	// ErrInvalidConfig indicates that the provided configuration is invalid
	ErrInvalidConfig = errors.New("invalid configuration")

	// ErrLLMGeneration indicates that LLM generation failed
	ErrLLMGeneration = errors.New("LLM generation failed")

	// ErrAnalysisFailed indicates that repository analysis failed
	ErrAnalysisFailed = errors.New("repository analysis failed")

	// ErrConfigGeneration indicates that config generation failed
	ErrConfigGeneration = errors.New("config generation failed")

	// ErrInvalidResponse indicates that the LLM response was invalid
	ErrInvalidResponse = errors.New("invalid LLM response")

	// ErrRepositoryNotFound indicates that the repository path does not exist
	ErrRepositoryNotFound = errors.New("repository not found")
)
