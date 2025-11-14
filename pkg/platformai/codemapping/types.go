package codemapping

// RepositoryAnalysis contains repository analysis results
type RepositoryAnalysis struct {
	PrimaryLanguage   string
	DetectedFramework string
	Files             []string
	Dependencies      map[string]string
	HasDockerfile     bool
	DockerfileContent string
	LanguageVersion   string
}

// PlatformConfig represents the generated platform configuration
type PlatformConfig struct {
	Service    ServiceConfig    `yaml:"service" json:"service"`
	Resources  ResourceConfig   `yaml:"resources" json:"resources"`
	Database   *DatabaseConfig  `yaml:"database,omitempty" json:"database,omitempty"`
	Cache      *CacheConfig     `yaml:"cache,omitempty" json:"cache,omitempty"`
	Monitoring MonitoringConfig `yaml:"monitoring" json:"monitoring"`
	Security   SecurityConfig   `yaml:"security" json:"security"`
}

// ServiceConfig contains service configuration
type ServiceConfig struct {
	Name      string `yaml:"name" json:"name"`
	Template  string `yaml:"template" json:"template"`
	Runtime   string `yaml:"runtime" json:"runtime"`
	Framework string `yaml:"framework" json:"framework"`
	Port      int    `yaml:"port" json:"port"`
}

// ResourceConfig contains resource allocation configuration
type ResourceConfig struct {
	CPU     string        `yaml:"cpu" json:"cpu"`
	Memory  string        `yaml:"memory" json:"memory"`
	Scaling ScalingConfig `yaml:"scaling" json:"scaling"`
}

// ScalingConfig contains auto-scaling configuration
type ScalingConfig struct {
	MinReplicas      int `yaml:"min_replicas" json:"min_replicas"`
	MaxReplicas      int `yaml:"max_replicas" json:"max_replicas"`
	TargetCPUPercent int `yaml:"target_cpu_percent" json:"target_cpu_percent"`
}

// DatabaseConfig contains database configuration
type DatabaseConfig struct {
	Type    string `yaml:"type" json:"type"`
	Version string `yaml:"version" json:"version"`
	Storage string `yaml:"storage" json:"storage"`
}

// CacheConfig contains cache configuration
type CacheConfig struct {
	Type    string `yaml:"type" json:"type"`
	Version string `yaml:"version" json:"version"`
	Memory  string `yaml:"memory" json:"memory"`
}

// MonitoringConfig contains monitoring configuration
type MonitoringConfig struct {
	Metrics bool `yaml:"metrics" json:"metrics"`
	Logs    bool `yaml:"logs" json:"logs"`
	Traces  bool `yaml:"traces" json:"traces"`
}

// SecurityConfig contains security configuration
type SecurityConfig struct {
	HealthCheck HealthCheckConfig `yaml:"health_check" json:"health_check"`
}

// HealthCheckConfig contains health check configuration
type HealthCheckConfig struct {
	Path string `yaml:"path" json:"path"`
	Port int    `yaml:"port" json:"port"`
}

// Recommendation represents an actionable recommendation
type Recommendation struct {
	Level   string `json:"level"` // "info", "warning", "critical"
	Title   string `json:"title"`
	Message string `json:"message"`
}
