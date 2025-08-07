// Package initialize handles the initialization of OpenList's configuration and components
package initialize

import (
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/shirou/gopsutil/v4/mem"
	log "github.com/sirupsen/logrus"

	"github.com/dongdio/OpenList/v4/drivers/base"
	"github.com/dongdio/OpenList/v4/global"
	"github.com/dongdio/OpenList/v4/internal/conf"
	"github.com/dongdio/OpenList/v4/utility/net"
	"github.com/dongdio/OpenList/v4/utility/utils"
)

const (
	// DefaultConfigFileName is the name of the configuration file
	DefaultConfigFileName = "config.json"

	// DefaultFileMode defines the permission for created files
	DefaultFileMode = 0o644

	// DefaultDirMode defines the permission for created directories
	DefaultDirMode = 0o755
)

// PWD returns the program working directory
func PWD() string {
	if global.ForceBinDir {
		ex, err := os.Executable()
		if err != nil {
			log.Fatal(err)

		}
		pwd := filepath.Dir(ex)
		return pwd
	}
	d, err := os.Getwd()
	if err != nil {
		d = "."
	}
	return d
}

// LastLaunchedVersion stores the version from the last launch
var LastLaunchedVersion string

// InitConfig initializes the application configuration
// It loads configuration from file or creates a default one if not exists,
// applies environment variables, and initializes related components
func InitConfig() {
	// Ensure data directory is absolute when force bin dir is enabled
	pwd := PWD()
	dataDir := global.DataDir
	if !filepath.IsAbs(dataDir) {
		global.DataDir = filepath.Join(pwd, global.DataDir)
	}

	// Construct config file path
	configPath := filepath.Join(global.DataDir, DefaultConfigFileName)
	log.Infof("reading config file: %s", configPath)

	if !utils.Exists(configPath) {
		createDefaultConfig(configPath)
	} else {
		loadExistingConfig(configPath)
	}

	// Configure concurrency limit if specified
	if conf.Conf.MaxConcurrency > 0 {
		net.DefaultConcurrencyLimit = &net.ConcurrencyLimit{Limit: conf.Conf.MaxConcurrency}
	}

	if conf.Conf.MaxBufferLimit < 0 {
		m, _ := mem.VirtualMemory()
		if m != nil {
			conf.MaxBufferLimit = max(int(float64(m.Total)*0.05), 4*utils.MB)
			conf.MaxBufferLimit -= conf.MaxBufferLimit % utils.MB
		} else {
			conf.MaxBufferLimit = 16 * utils.MB
		}
	} else {
		conf.MaxBufferLimit = conf.Conf.MaxBufferLimit * utils.MB
	}
	log.Infof("max buffer limit: %d", conf.MaxBufferLimit)

	if len(conf.Conf.Log.Filter.Filters) == 0 {
		conf.Conf.Log.Filter.Enable = false
	}

	// Ensure temp directory is absolute and exists
	ensureTempDirExists(pwd)

	log.Debugf("config: %+v", conf.Conf)

	// Initialize HTTP client and URL configuration
	base.InitClient()
	initURL()
}

// createDefaultConfig creates a default configuration file at the specified path
func createDefaultConfig(configPath string) {
	log.Info("config file does not exist, creating default config file")

	// Create the file and parent directories if needed
	_, err := utils.CreateNestedFile(configPath)
	if err != nil {
		log.Fatalf("failed to create config file: %v", err)
	}

	// Initialize with default configuration
	conf.Conf = conf.DefaultConfig(global.DataDir)
	LastLaunchedVersion = conf.Version
	conf.Conf.LastLaunchedVersion = conf.Version

	// Write the configuration to file
	if !utils.WriteJSONToFile(configPath, conf.Conf) {
		log.Fatal("failed to write default config file")
	}
}

// loadExistingConfig loads configuration from an existing file
func loadExistingConfig(configPath string) {
	// Read the configuration file
	configBytes, err := os.ReadFile(configPath)
	if err != nil {
		log.Fatalf("failed to read config file: %v", err)
	}

	// Parse the configuration
	conf.Conf = conf.DefaultConfig(global.DataDir)
	err = utils.JSONTool.Unmarshal(configBytes, conf.Conf)
	if err != nil {
		log.Fatalf("failed to parse config file: %v", err)
	}

	// Store last launched version and update if needed
	LastLaunchedVersion = conf.Conf.LastLaunchedVersion
	if strings.HasPrefix(conf.Version, "v") || LastLaunchedVersion == "" {
		conf.Conf.LastLaunchedVersion = conf.Version
	}

	// Update the config file to ensure it has the latest structure
	updateConfigFile(configPath)
}

// updateConfigFile writes the current configuration back to the file
func updateConfigFile(configPath string) {
	confBody, err := utils.JSONTool.MarshalIndent(conf.Conf, "", "  ")
	if err != nil {
		log.Fatalf("failed to marshal config: %v", err)
	}

	err = os.WriteFile(configPath, confBody, DefaultFileMode)
	if err != nil {
		log.Fatalf("failed to update config file: %v", err)
	}
}

// ensureTempDirExists ensures the temporary directory exists and is absolute
func ensureTempDirExists(pwd string) {
	convertAbsPath := func(path *string) {
		if !filepath.IsAbs(*path) {
			*path = filepath.Join(pwd, *path)

		}
	}
	convertAbsPath(&conf.Conf.TempDir)
	convertAbsPath(&conf.Conf.BleveDir)
	convertAbsPath(&conf.Conf.Log.Name)
	convertAbsPath(&conf.Conf.Database.DBFile)
	if conf.Conf.DistDir != "" {
		convertAbsPath(&conf.Conf.DistDir)
	}

	// Create the directory if it doesn't exist
	err := os.MkdirAll(conf.Conf.TempDir, DefaultDirMode)
	if err != nil {
		log.Fatalf("failed to create temp directory: %v", err)
	}
}

// initURL parses and validates the site URL configuration
func initURL() {
	// Ensure URL has a scheme
	siteURL := conf.Conf.SiteURL
	if !strings.Contains(siteURL, "://") {
		siteURL = utils.FixAndCleanPath(siteURL)
	}

	// Parse the URL
	u, err := url.Parse(siteURL)
	if err != nil {
		log.Fatalf("failed to parse site_url '%s': %v", siteURL, err)
	}

	// Store the parsed URL
	conf.URL = u
}

// CleanTempDir removes all files from the temporary directory
func CleanTempDir() {
	files, err := os.ReadDir(conf.Conf.TempDir)
	if err != nil {
		log.Errorf("failed to list temp files: %v", err)
		return
	}

	for _, file := range files {
		filePath := filepath.Join(conf.Conf.TempDir, file.Name())
		if err = os.RemoveAll(filePath); err != nil {
			log.Errorf("failed to delete temp file '%s': %v", filePath, err)
		}
	}
}