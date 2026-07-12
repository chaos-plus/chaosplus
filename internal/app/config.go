package app

import (
	"time"

	"github.com/chaos-plus/chaosplus/internal/core/extension/bunx"
	"github.com/chaos-plus/chaosplus/internal/infra/geoip"
)

// 类似 springboot 的配置, 由koanf实现外部配置加载
type Config struct {
	Name        string                     `mapstructure:"name" description:"app name" default:""`
	Debug       bool                       `mapstructure:"debug" short:"d" description:"debug mode" default:"false"`
	Timezone    string                     `mapstructure:"timezone" description:"timezone" default:"UTC"`
	WorkerLease int                        `mapstructure:"worker_lease" description:"guid worker-id lease seconds; heartbeat renews at a third of this" default:"3600"`
	Log         Log                        `mapstructure:"log" group:"log"`
	RestServer  RestServer                 `mapstructure:"rest" group:"rest"`
	GrpcServer  GrpcServer                 `mapstructure:"grpc" group:"grpc"`
	Redis       Redis                      `mapstructure:"redis" group:"redis"`
	RateLimit   RateLimit                  `mapstructure:"ratelimit" group:"ratelimit"`
	Database    map[string]bunx.Datasource `mapstructure:"database" group:"database" mapkey:"<dbkey>"`
	GeoIP       geoip.Config               `mapstructure:"geoip" group:"geoip"`
}

// Redis configures the shared Redis client, supporting standalone, sentinel, and
// cluster deployments via go-redis's universal client:
//   - one Addrs entry, no MasterName     → standalone
//   - MasterName set (Addrs = sentinels) → sentinel / failover
//   - multiple Addrs, no MasterName      → cluster
//
// Empty Addrs disables Redis (and therefore rate limiting).
type Redis struct {
	Addrs      []string `mapstructure:"addrs" description:"redis addresses host:port (1=standalone, N=cluster, or sentinel addrs); empty disables redis"`
	MasterName string   `mapstructure:"master_name" description:"sentinel master name; set to use sentinel/failover" default:""`
	Username   string   `mapstructure:"username" description:"redis username" default:""`
	Password   string   `mapstructure:"password" description:"redis password" default:""`
	DB         int      `mapstructure:"db" description:"redis database index" default:"0"`
}

// RateLimit configures the Redis-backed rate limiter. It enforces per-IP and
// per-account dimensions independently; each is applied only when enabled with a
// positive rate. Requires a configured Redis; disabled otherwise.
type RateLimit struct {
	Enabled bool        `mapstructure:"enabled" description:"enable rate limiting (requires redis)" default:"false"`
	Prefix  string      `mapstructure:"prefix" description:"redis key prefix" default:"rl"`
	IP      RateRule    `mapstructure:"ip" group:"ip"`
	Account AccountRule `mapstructure:"account" group:"account"`
}

// RateRule is one GCRA rate-limit rule: rate requests per period, allowing bursts
// up to burst (defaults to rate when zero).
type RateRule struct {
	Enabled bool          `mapstructure:"enabled" description:"enable this dimension" default:"false"`
	Rate    int           `mapstructure:"rate" description:"allowed requests per period" default:"0"`
	Period  time.Duration `mapstructure:"period" description:"rate window, e.g. 1m" default:"1m"`
	Burst   int           `mapstructure:"burst" description:"max burst; defaults to rate when 0" default:"0"`
}

// AccountRule is a RateRule plus the header carrying the account id. When auth is
// added, the account key can move from this header to the request context.
type AccountRule struct {
	Enabled bool          `mapstructure:"enabled" description:"enable this dimension" default:"false"`
	Rate    int           `mapstructure:"rate" description:"allowed requests per period" default:"0"`
	Period  time.Duration `mapstructure:"period" description:"rate window, e.g. 1m" default:"1m"`
	Burst   int           `mapstructure:"burst" description:"max burst; defaults to rate when 0" default:"0"`
	Header  string        `mapstructure:"header" description:"header carrying the account id" default:"X-Account-Id"`
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
