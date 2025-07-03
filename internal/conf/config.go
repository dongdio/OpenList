package conf

import (
	"path/filepath"

	"github.com/dongdio/OpenList/v4/global"
	"github.com/dongdio/OpenList/v4/utility/utils/random"
)

type Database struct {
	Type        string `json:"type"`
	Host        string `json:"host"`
	Port        int    `json:"port"`
	User        string `json:"user"`
	Password    string `json:"password"`
	Name        string `json:"name"`
	DBFile      string `json:"db_file"`
	TablePrefix string `json:"table_prefix"`
	SSLMode     string `json:"ssl_mode"`
	DSN         string `json:"dsn"`
}

type Meilisearch struct {
	Host        string `json:"host"`
	APIKey      string `json:"api_key"`
	IndexPrefix string `json:"index_prefix"`
}

type Scheme struct {
	Address      string `json:"address"`
	HttpPort     int    `json:"http_port"`
	HttpsPort    int    `json:"https_port"`
	ForceHttps   bool   `json:"force_https"`
	CertFile     string `json:"cert_file"`
	KeyFile      string `json:"key_file"`
	UnixFile     string `json:"unix_file"`
	UnixFilePerm string `json:"unix_file_perm"`
	EnableH2c    bool   `json:"enable_h2c"`
}

type LogConfig struct {
	Enable     bool   `json:"enable"`
	Name       string `json:"name"`
	MaxSize    int    `json:"max_size"`
	MaxBackups int    `json:"max_backups"`
	MaxAge     int    `json:"max_age"`
	Compress   bool   `json:"compress"`
}

type TaskConfig struct {
	Workers        int  `json:"workers"`
	MaxRetry       int  `json:"max_retry"`
	TaskPersistant bool `json:"task_persistant"`
}

type TasksConfig struct {
	Download           TaskConfig `json:"download"`
	Transfer           TaskConfig `json:"transfer"`
	Upload             TaskConfig `json:"upload"`
	Copy               TaskConfig `json:"copy"`
	Move               TaskConfig `json:"move"`
	Decompress         TaskConfig `json:"decompress"`
	DecompressUpload   TaskConfig `json:"decompress_upload"`
	AllowRetryCanceled bool       `json:"allow_retry_canceled"`
}

type Cors struct {
	AllowOrigins []string `json:"allow_origins"`
	AllowMethods []string `json:"allow_methods"`
	AllowHeaders []string `json:"allow_headers"`
}

type S3 struct {
	Enable bool `json:"enable"`
	Port   int  `json:"port"`
	SSL    bool `json:"ssl"`
}

type FTP struct {
	Enable                  bool   `json:"enable"`
	Listen                  string `json:"listen"`
	FindPasvPortAttempts    int    `json:"find_pasv_port_attempts"`
	ActiveTransferPortNon20 bool   `json:"active_transfer_port_non_20"`
	IdleTimeout             int    `json:"idle_timeout"`
	ConnectionTimeout       int    `json:"connection_timeout"`
	DisableActiveMode       bool   `json:"disable_active_mode"`
	DefaultTransferBinary   bool   `json:"default_transfer_binary"`
	EnableActiveConnIPCheck bool   `json:"enable_active_conn_ip_check"`
	EnablePasvConnIPCheck   bool   `json:"enable_pasv_conn_ip_check"`
}

type SFTP struct {
	Enable bool   `json:"enable"`
	Listen string `json:"listen"`
}

type Config struct {
	Force                 bool        `json:"force"`
	SiteURL               string      `json:"site_url"`
	Cdn                   string      `json:"cdn"`
	JwtSecret             string      `json:"jwt_secret"`
	TokenExpiresIn        int         `json:"token_expires_in"`
	Database              Database    `json:"database"`
	Meilisearch           Meilisearch `json:"meilisearch"`
	Scheme                Scheme      `json:"scheme"`
	TempDir               string      `json:"temp_dir"`
	BleveDir              string      `json:"bleve_dir"`
	DistDir               string      `json:"dist_dir"`
	Log                   LogConfig   `json:"log"`
	DelayedStart          int         `json:"delayed_start"`
	MaxConnections        int         `json:"max_connections"`
	MaxConcurrency        int         `json:"max_concurrency"`
	TlsInsecureSkipVerify bool        `json:"tls_insecure_skip_verify"`
	Tasks                 TasksConfig `json:"tasks"`
	Cors                  Cors        `json:"cors"`
	S3                    S3          `json:"s3"`
	FTP                   FTP         `json:"ftp"`
	SFTP                  SFTP        `json:"sftp"`
	LastLaunchedVersion   string      `json:"last_launched_version"`
}

func DefaultConfig() *Config {
	tempDir := filepath.Join(global.DataDir, "temp")
	indexDir := filepath.Join(global.DataDir, "bleve")
	logPath := filepath.Join(global.DataDir, "log/log.log")
	dbPath := filepath.Join(global.DataDir, "data.db")
	return &Config{
		Scheme: Scheme{
			Address:    "0.0.0.0",
			UnixFile:   "",
			HttpPort:   5244,
			HttpsPort:  -1,
			ForceHttps: false,
			CertFile:   "",
			KeyFile:    "",
		},
		JwtSecret:      random.String(16),
		TokenExpiresIn: 48,
		TempDir:        tempDir,
		Database: Database{
			Type:        "sqlite3",
			Port:        0,
			TablePrefix: "x_",
			DBFile:      dbPath,
		},
		Meilisearch: Meilisearch{
			Host: "http://localhost:7700",
		},
		BleveDir: indexDir,
		Log: LogConfig{
			Enable:     true,
			Name:       logPath,
			MaxSize:    50,
			MaxBackups: 30,
			MaxAge:     28,
		},
		MaxConnections:        0,
		MaxConcurrency:        64,
		TlsInsecureSkipVerify: true,
		Tasks: TasksConfig{
			Download: TaskConfig{
				Workers:  5,
				MaxRetry: 1,
				// TaskPersistant: true,
			},
			Transfer: TaskConfig{
				Workers:  5,
				MaxRetry: 2,
				// TaskPersistant: true,
			},
			Upload: TaskConfig{
				Workers: 5,
			},
			Copy: TaskConfig{
				Workers:  5,
				MaxRetry: 2,
				// TaskPersistant: true,
			},
			Move: TaskConfig{
				Workers:  5,
				MaxRetry: 2,
				// TaskPersistant: true,
			},
			Decompress: TaskConfig{
				Workers:  5,
				MaxRetry: 2,
				// TaskPersistant: true,
			},
			DecompressUpload: TaskConfig{
				Workers:  5,
				MaxRetry: 2,
			},
			AllowRetryCanceled: false,
		},
		Cors: Cors{
			AllowOrigins: []string{"*"},
			AllowMethods: []string{"*"},
			AllowHeaders: []string{"*"},
		},
		S3: S3{
			Enable: false,
			Port:   5246,
			SSL:    false,
		},
		FTP: FTP{
			Enable:                  false,
			Listen:                  ":5221",
			FindPasvPortAttempts:    50,
			ActiveTransferPortNon20: false,
			IdleTimeout:             900,
			ConnectionTimeout:       30,
			DisableActiveMode:       false,
			DefaultTransferBinary:   false,
			EnableActiveConnIPCheck: true,
			EnablePasvConnIPCheck:   true,
		},
		SFTP: SFTP{
			Enable: false,
			Listen: ":5222",
		},
		LastLaunchedVersion: "",
	}
}