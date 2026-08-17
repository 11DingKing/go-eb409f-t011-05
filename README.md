# 额济纳旗微电网调度服务

一个自包含的 Go 后端服务，用于在并网/离网切换窗口内统筹构网型储能舱区的
黑启动演练与日常运维，确保黑启动演练、储能舱巡检、设备异常隔离与维修工单
四条流程互不干扰。

## 业务模型

系统维护三本台账并串联四条流程：

- 台账：构网型储能舱区、黑启动预案库、巡检工单池（含异常与维修工单）。
- 流程：黑启动演练申请与执行、储能电池舱每日巡检、设备异常上报与隔离、
  维修工单派发与完工复测。

关键调度规则：

- 演练前须锁定目标舱区作业锁，同一舱区同时只允许一个班组持锁。
- 异常上报必须先完成电气隔离，再生成维修工单。
- 巡检发现的缺陷须在 24 小时内转派检修班组（由后台调度器巡检 SLA）。
- 维修完工须复测同期并网条件并经调度班长确认后方可销单。
- 离网窗口一旦被演练占用，相关巡检与维修工单自动改期，窗口释放后自动恢复。
- 演练中主电源突然恢复造成同期抢合闸时，取消剩余步骤并回退到黑启动预案的
  备用路径；储能舱失电时按应急清单重启舱控并重新执行同期。

## 技术栈

- Go 1.26，仅使用标准库，无外部依赖。
- HTTP 接入（`net/http` 路由）、应用编排（`internal/app`）、领域状态
  （`internal/domain`）、文件快照持久化（`internal/store`）、后台任务
  （`internal/scheduler`）。

## 目录结构

```
cmd/server/            程序入口，加载配置、播种默认数据、启动 HTTP 与调度器
internal/domain/cabin  储能舱区台账与作业锁（并发边界）
internal/domain/blackstart 黑启动预案与演练状态机（主路径/备用路径/失电恢复）
internal/domain/workorder 巡检/异常/维修工单状态机与关联
internal/store         内存+JSON快照持久化，实现各领域仓储接口
internal/app           应用编排：锁、离网窗口、改期、幂等与失败恢复
internal/scheduler     后台任务：24小时 SLA 巡检与改期工单恢复
internal/httpsrv       HTTP 路由与错误码映射
config.json            服务配置
```

## 配置

`config.json`：

| 字段                | 说明                 | 默认值                              |
|---------------------|----------------------|-------------------------------------|
| `listen_addr`       | 监听地址             | `:48302`                            |
| `data_path`         | 状态快照文件路径     | `/tmp/microgrid-dispatch-state.json`|
| `scheduler_interval`| 后台巡检周期         | `30s`                               |

环境变量 `LISTEN_ADDR`、`DATA_PATH` 可覆盖对应配置。

## 本地运行

```bash
go run ./cmd/server
# 服务监听 :48302
```

健康检查：

```bash
curl http://localhost:48302/healthz
```

## 主要接口

| 方法 | 路径                                  | 说明                       |
|------|---------------------------------------|----------------------------|
| GET  | `/healthz`                            | 健康检查                   |
| GET  | `/api/cabins`                         | 列出舱区                   |
| POST | `/api/cabins`                         | 登记舱区                   |
| GET  | `/api/cabins/{id}`                    | 查询舱区                   |
| POST | `/api/cabins/{id}/restart`            | 失电后重启舱控             |
| GET  | `/api/plans`                          | 列出黑启动预案             |
| POST | `/api/plans`                          | 登记黑启动预案             |
| POST | `/api/drills`                         | 申请黑启动演练             |
| GET  | `/api/drills/{id}`                    | 查询演练                   |
| POST | `/api/drills/{id}/approve`            | 调度班长批准并锁定舱区     |
| POST | `/api/drills/{id}/start`              | 开始执行（主路径）         |
| POST | `/api/drills/{id}/execute`            | 执行下一个步骤             |
| POST | `/api/drills/{id}/complete`           | 完成演练                   |
| POST | `/api/drills/{id}/cancel`             | 取消演练                   |
| POST | `/api/drills/{id}/power-recovery`     | 主电源恢复，回退备用路径   |
| POST | `/api/drills/{id}/cabin-powerloss`    | 舱区失电，暂停等待重启     |
| POST | `/api/drills/{id}/resume`             | 重启后恢复演练             |
| POST | `/api/inspections`                    | 创建巡检工单               |
| POST | `/api/inspections/{id}/complete`      | 完成巡检（可登记缺陷）     |
| POST | `/api/anomalies`                      | 上报设备异常               |
| POST | `/api/anomalies/{id}/isolate`         | 完成电气隔离               |
| POST | `/api/anomalies/{id}/repair`          | 由异常生成维修工单         |
| POST | `/api/repairs/{id}/dispatch`          | 派发维修工单               |
| POST | `/api/repairs/{id}/complete`          | 维修完工                   |
| POST | `/api/repairs/{id}/retest`            | 同期并网复测               |
| POST | `/api/repairs/{id}/confirm`           | 调度班长确认销单           |
| GET  | `/api/orders/{id}`                    | 查询工单                   |

错误码：404 实体不存在，409 锁冲突/非法状态迁移/未隔离/未复测，
400 请求参数缺失，500 其他内部错误。

## 测试

```bash
go test -timeout=120s -count=1 ./...
```

测试覆盖正常路径、错误路径、状态迁移、并发抢锁与取消、以及主电源恢复回退、
舱区失电重启等失败恢复场景，全部基于内存运行，不依赖外部服务。

## Docker

构建（支持 amd64 与 arm64）：

```bash
docker build -t microgrid-dispatch:latest .
```

运行：

```bash
docker run --rm -p 48302:48302 microgrid-dispatch:latest
```

多架构构建：

```bash
docker buildx build --platform linux/amd64,linux/arm64 -t microgrid-dispatch:latest .
```
