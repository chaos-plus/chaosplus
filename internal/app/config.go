package app

import "github.com/chaos-plus/chaosplus/internal/core/extension/bunx"

// 类似 springboot 的配置, 由koanf实现外部配置加载
type Config struct {
	Name       string                     `mapstructure:"name" description:"app name" default:""`
	Debug      bool                       `mapstructure:"debug" short:"d" description:"debug mode" default:"false"`
	Timezone   string                     `mapstructure:"timezone" description:"timezone" default:"UTC"`
	Log        Log                        `mapstructure:"log" group:"log"`
	RestServer RestServer                 `mapstructure:"rest" group:"rest"`
	GrpcServer GrpcServer                 `mapstructure:"grpc" group:"grpc"`
	Database   map[string]bunx.Datasource `mapstructure:"database" group:"database" mapkey:"<dbkey>"`
}

type Log struct {
	// File is the log file path. Empty disables file logging (stdout only).
	File string `mapstructure:"file" description:"log file path, empty for stdout only" default:"logs/app.log"`

	// Level is the minimum log level to write to the file.
	Level string `mapstructure:"level" description:"log level" default:"info"`

	// Format is the log format (json or text).
	Format string `mapstructure:"format" description:"log format" default:"json"`

	// MaxSize is the maximum size in megabytes of the log file before it gets rotated.
	MaxSize int `mapstructure:"max_size" description:"max size of log file in MB" default:"10"`

	// RotateSize is the size of file to trigger rotation (MB).
	RotateSize int `mapstructure:"rotate_size" description:"size of file to trigger rotation in MB" default:"10"`

	// RotateBackups is the maximum number of old log files to keep.
	RotateBackups int `mapstructure:"rotate_backups" description:"max number of old log files to keep" default:"5"`
}

type RestServer struct {
	Host string `mapstructure:"host" description:"host" default:"0.0.0.0"`
	Port int    `mapstructure:"port" description:"http/rest port" default:"8080"`
}

type GrpcServer struct {
	Host string `mapstructure:"host" description:"host" default:"0.0.0.0"`
	Port int    `mapstructure:"port" description:"grpc port" default:"9090"`
}
