# 项目结构

``` 
├── main.go                     # 项目总入口，直接启动 API 服务
├── go.mod                      # 依赖管理
├── Makefile                    # 常用指令 (make run, make build, make test)
├── .env                        # 环境变量
├── config/                     # 配置管理
│   ├── config.go               # 配置结构体
├── internal/                   # 核心私有业务逻辑 (Internal 机制保证安全)
│   ├── app/                    # 应用初始化 (可选，用于进一步解耦 main.go)
│   │   └── app.go
│   ├── entity/                 # 【领域实体层】纯净的业务结构体，无 tag，无依赖
│   │   └── user.go
│   ├── services/               # 【业务逻辑层】
│   │   ├── interfaces.go       # 定义 Service 接口 (供 Handler 调用)
│   │   └── user_service.go     # 具体业务实现 (协调 Repository 和 Cache)
│   ├── repository/             # 【数据访问层】
│   │   ├── dao/                # Data Access Object (GORM 模型/标签)
│   │   │   └── user_dao.go     
│   │   ├── interfaces.go       # 定义 Repo 接口 (供 Service 调用)
│   │   ├── user_repo.go        # MySQL/Postgres 具体实现
│   │   └── cache_repo.go       # Redis 具体实现
│   ├── handlers/               # 【HTTP 处理层】
│   │   ├── dto/                # Data Transfer Object (API 输入输出定义)
│   │   │   ├── user_request.go # 请求参数校验标签
│   │   │   └── user_response.go# 返回给前端的 JSON 格式
│   │   └── user_handler.go     # 接收 Fiber 这里的 context (*fiber.Ctx)
│   └── database/               # 基础设施层
│       ├── mysql.go            # GORM 连接池初始化
│       └── redis.go            # Redis 客户端初始化
├── middleware/                 # Fiber 中间件
│   ├── auth.go                 # JWT 校验
│   └── logger.go               # 日志拦截
├── router/                     # 路由注册
│   └── router.go               # 路由分组与路径定义
└── pkg/                        # 外部公共库 (可导出的工具函数)
    ├── logger/                 # 自定义日志封装
    └── utils/                  # 字符串处理、加解密等

```