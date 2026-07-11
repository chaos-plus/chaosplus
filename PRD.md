# ChaosPlus API PRD

## Tech Stack

### Huma

1. 使用Huma提供路由
2. 使用Huma的OpenAPI 3.0生成API文档
3. 使用Huma的请求验证和响应序列化
4. 使用Huma的错误处理



### Chi

1. 使用Chi提供路由
2. 使用Chi的中间件系统
3. 使用Chi的路由匹配



### Goose

1. 使用Goose提供数据库迁移
2. 需要同时支持 sqlite、mysql、postgresql
3. 需要支持 up、down 操作


### Bun

1. 使用Bun作为数据库ORM
2. 需要同时支持 sqlite、mysql、postgresql


## Project Structure


- cmd
    - chaosplus-server
        - main.go (main entry point)
- internal
    - app
        - app.go (application entry point)
        - config.go (application configuration)
        - bootstrap.go (application bootstrap)
    - core
        - model (common model)
            - guid.go 
                - snowflake id type
            - response.go
                - common res model
                    - code
                    - message
                    - meta
                    - data
            - pagination.go
                - common page req model
                    - offset
                    - limit
                    - filter
                        - filter[key]=condition:value
                    - sort
                        - sort[key]=asc|desc
                - common page res model (res.data)
                    - []interface{}
        - extension
            - goose
            - huma
                - otel logger
            - bun
                - otel logger
        - middleware
            - cors.go
            - authn.go
            - authz.go
            - ratelimit.go
            - logging.go
            - recovery.go
    - modules
        - {module}  (ddd-lite)
            - i18n
                - en-US.json
                - zh-CN.json
                - xx-XX.json
            - api
                - rest.go
                - grpc.go
            - proto
                - api/v1
                    - {module}.proto
                - gen
                    - {module}_grpc.pb.go
                    - {module}.pb.go
            - db
                - sqlite
                - mysql
                - postgresql
            - sql
                - sqlite
                - mysql
                - postgresql
            - service
            - domain
            - repository
- pkg (common package)
    - logger
    - geoip
        - geolite2
        - ip2location
        - ip2region
    - terminal
    - importer/exporter (data)
        - csv
        - json
        - xml
        - excel
        - pdf
    - http
        - clientip
    - interpreter
        - go: yaegi
        - lua: gopher-lua
        - wasm: wazero
        - js: goja
    - sysinfo
        - cpu
        - memory
        - disk
        - network
        - os
        - process
        - user
        - timezone
        - locale
        - hostname
        - uptime
        - loadavg
        - boottime
        - platform
        - kernel
        - kernel_version
        - kernel_arch
        - kernel_machine
        - kernel_platform
        - kernel_version
        - kernel_machine
        - kernel_platform
        - kernel_version
        - kernel_machine
        - kernel_platform
    - utils
- go.mod
- go.sum
- buf.gen.yaml
- buf.yaml